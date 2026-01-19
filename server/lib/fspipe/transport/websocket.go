package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
	"github.com/onkernel/kernel-images/server/lib/fspipe/queue"
)

// WebSocketClient manages WebSocket connection to the remote listener
type WebSocketClient struct {
	url    string
	config ClientConfig

	connMu sync.RWMutex
	conn   *websocket.Conn

	state atomic.Int32 // ConnectionState

	// Message queue for non-blocking sends
	sendQueue *queue.Queue

	// Response channel for SendAndReceive - protected by reqMu
	responseCh chan wsResponse
	reqMu      sync.Mutex // Serializes SendAndReceive calls to prevent response mixup

	// Background goroutine management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Reconnection management - single goroutine handles all reconnects
	reconnectCh   chan struct{}
	reconnectOnce sync.Once

	// Shutdown management
	shutdownMu sync.Mutex
	shutdown   bool

	// Metrics
	messagesSent     atomic.Uint64
	messagesAcked    atomic.Uint64
	messagesRetried  atomic.Uint64
	connectionLost   atomic.Uint64
	reconnectSuccess atomic.Uint64
	healthCheckFails atomic.Uint64
}

type wsResponse struct {
	msgType byte
	data    []byte
	err     error
}

// NewWebSocketClient creates a new WebSocket transport client
func NewWebSocketClient(url string, config ClientConfig) *WebSocketClient {
	// Apply defaults for zero values
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &WebSocketClient{
		url:    url,
		config: config,
		ctx:    ctx,
		cancel: cancel,
		sendQueue: queue.New(queue.Config{
			MaxSize:    config.QueueSize,
			AckTimeout: config.AckTimeout,
			MaxRetries: 3,
		}),
		responseCh:  make(chan wsResponse, 10),
		reconnectCh: make(chan struct{}, 1),
	}

	c.state.Store(int32(StateDisconnected))
	return c
}

// Connect establishes WebSocket connection
func (c *WebSocketClient) Connect() error {
	c.connMu.Lock()
	err := c.connectLocked()
	c.connMu.Unlock()

	if err != nil {
		return err
	}

	// Start background workers exactly once
	c.reconnectOnce.Do(func() {
		c.wg.Add(4)
		go c.sendLoop()
		go c.readLoop()
		go c.pingLoop()
		go c.reconnectLoop()
	})

	return nil
}

func (c *WebSocketClient) connectLocked() error {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.state.Store(int32(StateConnecting))

	backoff := c.config.InitialBackoff
	attempt := 0

	dialer := websocket.Dialer{
		HandshakeTimeout: c.config.DialTimeout,
	}

	for {
		select {
		case <-c.ctx.Done():
			c.state.Store(int32(StateDisconnected))
			return c.ctx.Err()
		default:
		}

		attempt++

		conn, resp, err := dialer.Dial(c.url, http.Header{})
		if err != nil {
			if resp != nil {
				logging.Warn("WebSocket dial attempt %d failed: %v (status: %d)", attempt, err, resp.StatusCode)
			} else {
				logging.Warn("WebSocket dial attempt %d failed: %v", attempt, err)
			}

			if c.config.MaxRetries > 0 && attempt >= c.config.MaxRetries {
				c.state.Store(int32(StateFailed))
				return fmt.Errorf("failed to connect after %d retries: %w", attempt, err)
			}

			// Exponential backoff with context cancellation
			timer := time.NewTimer(backoff)
			select {
			case <-c.ctx.Done():
				timer.Stop()
				c.state.Store(int32(StateDisconnected))
				return c.ctx.Err()
			case <-timer.C:
			}

			backoff = time.Duration(float64(backoff) * c.config.BackoffMultiplier)
			if backoff > c.config.MaxBackoff {
				backoff = c.config.MaxBackoff
			}
			continue
		}

		c.conn = conn
		c.state.Store(int32(StateConnected))
		logging.Info("WebSocket connected to %s (attempt %d)", c.url, attempt)
		c.reconnectSuccess.Add(1)
		return nil
	}
}

// reconnectLoop handles reconnection in a dedicated goroutine
func (c *WebSocketClient) reconnectLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.reconnectCh:
			// Drain any additional reconnect signals
			for {
				select {
				case <-c.reconnectCh:
				default:
					goto doReconnect
				}
			}

		doReconnect:
			currentState := ConnectionState(c.state.Load())
			if currentState == StateConnected {
				continue
			}

			c.connectionLost.Add(1)
			logging.Info("WebSocket starting reconnection...")

			c.connMu.Lock()
			if c.conn != nil {
				c.conn.Close()
				c.conn = nil
			}
			c.state.Store(int32(StateReconnecting))

			count := c.sendQueue.RetryPending()
			if count > 0 {
				logging.Info("Re-queued %d pending messages for retry", count)
			}

			err := c.connectLocked()
			c.connMu.Unlock()

			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				logging.Error("WebSocket reconnection failed: %v", err)
			}
		}
	}
}

// triggerReconnect signals the reconnect loop to reconnect
func (c *WebSocketClient) triggerReconnect() {
	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}
}

// sendLoop processes the message queue
func (c *WebSocketClient) sendLoop() {
	defer c.wg.Done()

	for {
		msg, err := c.sendQueue.Dequeue(c.ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, queue.ErrQueueClosed) {
				return
			}
			logging.Error("Dequeue error: %v", err)
			continue
		}

		err = c.sendMessage(msg)
		if err != nil {
			logging.Debug("Send failed for message %d: %v", msg.ID, err)
			c.handleSendError(msg, err)
		} else {
			c.messagesSent.Add(1)
		}
	}
}

func (c *WebSocketClient) sendMessage(msg *queue.Message) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return ErrNotConnected
	}

	// Build frame: [Length: 4B] [Type: 1B] [Payload: NB]
	payload, err := json.Marshal(msg.Payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	frameLen := uint32(1 + len(payload))
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, frameLen)
	buf.WriteByte(msg.Type)
	buf.Write(payload)

	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.BinaryMessage, buf.Bytes()); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// For messages expecting ACK, track them
	if msg.Type == protocol.MsgWriteChunk {
		c.sendQueue.TrackPending(msg)
	} else {
		select {
		case msg.Result <- nil:
		default:
		}
	}

	return nil
}

// readLoop reads messages from WebSocket
func (c *WebSocketClient) readLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.connMu.RLock()
		conn := c.conn
		c.connMu.RUnlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logging.Info("WebSocket closed normally")
			} else if !errors.Is(err, context.Canceled) {
				logging.Warn("WebSocket read error: %v", err)
				c.state.Store(int32(StateReconnecting))
				c.triggerReconnect()
			}
			continue
		}

		if messageType != websocket.BinaryMessage {
			continue
		}

		// Parse frame
		if len(data) < 5 {
			logging.Warn("Invalid frame: too short")
			continue
		}

		frameLen := binary.BigEndian.Uint32(data[:4])
		if int(frameLen) != len(data)-4 {
			logging.Warn("Invalid frame length")
			continue
		}

		msgType := data[4]
		payload := data[5:]

		// Handle ACK messages
		if msgType == protocol.MsgWriteAck {
			var ack protocol.WriteAck
			if err := json.Unmarshal(payload, &ack); err == nil {
				c.messagesAcked.Add(1)
			}
		}

		// Send to response channel for SendAndReceive
		select {
		case c.responseCh <- wsResponse{msgType: msgType, data: payload}:
		default:
		}
	}
}

// pingLoop sends periodic pings
func (c *WebSocketClient) pingLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheckInterval)
	defer ticker.Stop()

	consecutiveFails := 0
	const maxConsecutiveFails = 3

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if ConnectionState(c.state.Load()) != StateConnected {
				consecutiveFails = 0
				continue
			}

			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn != nil {
				conn.SetWriteDeadline(time.Now().Add(c.config.PingTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					consecutiveFails++
					c.healthCheckFails.Add(1)
					logging.Debug("Ping failed (%d/%d): %v", consecutiveFails, maxConsecutiveFails, err)

					if consecutiveFails >= maxConsecutiveFails {
						logging.Error("Ping failed %d times, triggering reconnect", consecutiveFails)
						c.state.Store(int32(StateReconnecting))
						c.triggerReconnect()
						consecutiveFails = 0
					}
				} else {
					consecutiveFails = 0
				}
			}
		}
	}
}

func (c *WebSocketClient) handleSendError(msg *queue.Message, err error) {
	// Trigger reconnection (non-blocking)
	c.triggerReconnect()

	msg.Retries++
	if msg.Retries <= 3 {
		c.messagesRetried.Add(1)
		if _, qerr := c.sendQueue.Enqueue(c.ctx, msg.Type, msg.Payload); qerr != nil {
			select {
			case msg.Result <- fmt.Errorf("requeue failed: %w", qerr):
			default:
			}
		}
	} else {
		select {
		case msg.Result <- fmt.Errorf("max retries exceeded: %w", err):
		default:
		}
	}
}

// Send sends a message asynchronously
func (c *WebSocketClient) Send(msgType byte, payload interface{}) error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	_, err := c.sendQueue.Enqueue(c.ctx, msgType, payload)
	return err
}

// SendSync sends a message and waits for send completion
func (c *WebSocketClient) SendSync(msgType byte, payload interface{}) error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	return c.sendQueue.EnqueueSync(c.ctx, msgType, payload)
}

// SendAndReceive sends a message and waits for response
func (c *WebSocketClient) SendAndReceive(msgType byte, payload interface{}) (byte, []byte, error) {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return 0, nil, ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	// Serialize all SendAndReceive calls to prevent response mixup
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	// Drain any stale responses from previous timed-out requests
	for {
		select {
		case <-c.responseCh:
			logging.Debug("SendAndReceive: drained stale response")
		default:
			goto sendRequest
		}
	}

sendRequest:
	c.connMu.Lock()

	if c.conn == nil {
		c.connMu.Unlock()
		return 0, nil, ErrNotConnected
	}

	// Build and send frame
	data, err := json.Marshal(payload)
	if err != nil {
		c.connMu.Unlock()
		return 0, nil, fmt.Errorf("marshal: %w", err)
	}

	frameLen := uint32(1 + len(data))
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, frameLen)
	buf.WriteByte(msgType)
	buf.Write(data)

	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.BinaryMessage, buf.Bytes()); err != nil {
		c.connMu.Unlock()
		go c.triggerReconnect()
		return 0, nil, fmt.Errorf("write: %w", err)
	}

	c.connMu.Unlock()

	// Wait for response
	select {
	case resp := <-c.responseCh:
		c.messagesAcked.Add(1)
		return resp.msgType, resp.data, resp.err
	case <-time.After(c.config.AckTimeout):
		return 0, nil, errors.New("response timeout")
	case <-c.ctx.Done():
		return 0, nil, c.ctx.Err()
	}
}

// State returns current connection state
func (c *WebSocketClient) State() ConnectionState {
	return ConnectionState(c.state.Load())
}

// Stats returns client statistics
func (c *WebSocketClient) Stats() map[string]uint64 {
	return map[string]uint64{
		"messages_sent":      c.messagesSent.Load(),
		"messages_acked":     c.messagesAcked.Load(),
		"messages_retried":   c.messagesRetried.Load(),
		"connection_lost":    c.connectionLost.Load(),
		"reconnect_success":  c.reconnectSuccess.Load(),
		"health_check_fails": c.healthCheckFails.Load(),
		"queue_length":       uint64(c.sendQueue.Len()),
		"pending_acks":       uint64(c.sendQueue.GetPendingCount()),
	}
}

// Close closes the WebSocket connection with graceful shutdown
func (c *WebSocketClient) Close() error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return nil
	}
	c.shutdown = true
	c.shutdownMu.Unlock()

	logging.Info("WebSocket client shutting down...")

	c.cancel()
	c.sendQueue.Close()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logging.Info("WebSocket client goroutines stopped gracefully")
	case <-time.After(c.config.ShutdownTimeout):
		logging.Warn("WebSocket client shutdown timed out after %v", c.config.ShutdownTimeout)
	}

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		err := c.conn.Close()
		c.conn = nil
		c.state.Store(int32(StateDisconnected))
		return err
	}
	c.state.Store(int32(StateDisconnected))
	return nil
}
