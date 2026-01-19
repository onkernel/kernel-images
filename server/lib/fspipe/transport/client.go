package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
	"github.com/onkernel/kernel-images/server/lib/fspipe/queue"
)

var (
	ErrNotConnected  = errors.New("not connected")
	ErrSendFailed    = errors.New("send failed")
	ErrShuttingDown  = errors.New("client is shutting down")
	ErrInvalidConfig = errors.New("invalid configuration")
)

// ConnectionState represents the connection status
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateFailed
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ClientConfig holds client configuration
type ClientConfig struct {
	// Connection settings
	DialTimeout       time.Duration
	MaxRetries        int // 0 = infinite retries
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64

	// Health check settings
	HealthCheckInterval time.Duration
	PingTimeout         time.Duration

	// Queue settings
	QueueSize  int
	AckTimeout time.Duration

	// Shutdown settings
	ShutdownTimeout time.Duration
}

// DefaultClientConfig returns production-ready defaults
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		DialTimeout:         10 * time.Second,
		MaxRetries:          0, // 0 = infinite retries
		InitialBackoff:      500 * time.Millisecond,
		MaxBackoff:          30 * time.Second,
		BackoffMultiplier:   2.0,
		HealthCheckInterval: 5 * time.Second,
		PingTimeout:         3 * time.Second,
		QueueSize:           1000,
		AckTimeout:          10 * time.Second, // Reduced from 30s
		ShutdownTimeout:     5 * time.Second,
	}
}

// ValidateConfig checks configuration for invalid values
func ValidateConfig(config ClientConfig) error {
	if config.DialTimeout <= 0 {
		return fmt.Errorf("%w: DialTimeout must be positive", ErrInvalidConfig)
	}
	if config.InitialBackoff <= 0 {
		return fmt.Errorf("%w: InitialBackoff must be positive", ErrInvalidConfig)
	}
	if config.MaxBackoff < config.InitialBackoff {
		return fmt.Errorf("%w: MaxBackoff must be >= InitialBackoff", ErrInvalidConfig)
	}
	if config.BackoffMultiplier < 1.0 {
		return fmt.Errorf("%w: BackoffMultiplier must be >= 1.0", ErrInvalidConfig)
	}
	if config.QueueSize <= 0 {
		return fmt.Errorf("%w: QueueSize must be positive", ErrInvalidConfig)
	}
	if config.AckTimeout <= 0 {
		return fmt.Errorf("%w: AckTimeout must be positive", ErrInvalidConfig)
	}
	if config.ShutdownTimeout <= 0 {
		return fmt.Errorf("%w: ShutdownTimeout must be positive", ErrInvalidConfig)
	}
	return nil
}

// Client manages the connection to the remote listener
type Client struct {
	addr   string
	config ClientConfig

	// Connection state protected by connMu
	connMu  sync.RWMutex
	conn    net.Conn
	encoder *protocol.Encoder
	decoder *protocol.Decoder
	bufW    *bufio.Writer

	state atomic.Int32 // ConnectionState

	// Message queue for non-blocking sends
	sendQueue *queue.Queue

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

// NewClient creates a new transport client with default config
func NewClient(addr string) *Client {
	return NewClientWithConfig(addr, DefaultClientConfig())
}

// NewClientWithConfig creates a new transport client with custom config
func NewClientWithConfig(addr string, config ClientConfig) *Client {
	// Apply defaults for zero values
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		addr:   addr,
		config: config,
		ctx:    ctx,
		cancel: cancel,
		sendQueue: queue.New(queue.Config{
			MaxSize:    config.QueueSize,
			AckTimeout: config.AckTimeout,
			MaxRetries: 3,
		}),
		reconnectCh: make(chan struct{}, 1), // Buffered to avoid blocking
	}

	c.state.Store(int32(StateDisconnected))
	return c
}

// Connect establishes connection to the remote server
func (c *Client) Connect() error {
	c.connMu.Lock()
	err := c.connectLocked()
	c.connMu.Unlock()

	if err != nil {
		return err
	}

	// Start background workers exactly once
	c.reconnectOnce.Do(func() {
		c.wg.Add(3)
		go c.sendLoop()
		go c.healthCheckLoop()
		go c.reconnectLoop()
	})

	return nil
}

// connectLocked establishes connection (must hold connMu)
func (c *Client) connectLocked() error {
	// Close existing connection if any
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.state.Store(int32(StateConnecting))

	backoff := c.config.InitialBackoff
	attempt := 0

	for {
		select {
		case <-c.ctx.Done():
			c.state.Store(int32(StateDisconnected))
			return c.ctx.Err()
		default:
		}

		attempt++
		conn, err := net.DialTimeout("tcp", c.addr, c.config.DialTimeout)
		if err != nil {
			logging.Warn("Connection attempt %d to %s failed: %v", attempt, c.addr, err)

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

		// Configure connection for reliability
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(15 * time.Second)
			tcpConn.SetNoDelay(true)
		}

		c.conn = conn
		c.bufW = bufio.NewWriterSize(conn, 64*1024)
		c.encoder = protocol.NewEncoder(c.bufW)
		c.decoder = protocol.NewDecoder(bufio.NewReaderSize(conn, 64*1024))

		c.state.Store(int32(StateConnected))
		logging.Info("Connected to %s (attempt %d)", c.addr, attempt)
		c.reconnectSuccess.Add(1)
		return nil
	}
}

// reconnectLoop handles reconnection in a dedicated goroutine
// This prevents race conditions and deadlocks from concurrent reconnection attempts
func (c *Client) reconnectLoop() {
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
				continue // Already connected
			}

			c.connectionLost.Add(1)
			logging.Info("Starting reconnection...")

			c.connMu.Lock()
			// Close existing connection
			if c.conn != nil {
				c.conn.Close()
				c.conn = nil
			}
			c.state.Store(int32(StateReconnecting))

			// Re-queue pending messages before reconnecting
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
				logging.Error("Reconnection failed: %v", err)
			}
		}
	}
}

// triggerReconnect signals the reconnect loop to reconnect
// This is safe to call from any goroutine without holding locks
func (c *Client) triggerReconnect() {
	// Non-blocking send to reconnect channel
	select {
	case c.reconnectCh <- struct{}{}:
	default:
		// Already a reconnect pending
	}
}

// sendLoop processes the message queue
func (c *Client) sendLoop() {
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

func (c *Client) sendMessage(msg *queue.Message) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return ErrNotConnected
	}

	// Set write deadline
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	if err := c.encoder.Encode(msg.Type, msg.Payload); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := c.bufW.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	// For messages expecting ACK, track them
	if msg.Type == protocol.MsgWriteChunk {
		c.sendQueue.TrackPending(msg)
	} else {
		// Non-ACK messages complete immediately
		select {
		case msg.Result <- nil:
		default:
		}
	}

	return nil
}

func (c *Client) handleSendError(msg *queue.Message, err error) {
	// Trigger reconnection (non-blocking, handled by reconnectLoop)
	c.triggerReconnect()

	// Re-queue the message with retry limit
	msg.Retries++
	if msg.Retries <= 3 {
		c.messagesRetried.Add(1)
		if _, qerr := c.sendQueue.Enqueue(c.ctx, msg.Type, msg.Payload); qerr != nil {
			// Queue full or closed, notify caller
			select {
			case msg.Result <- fmt.Errorf("requeue failed: %w", qerr):
			default:
			}
		}
	} else {
		// Max retries exceeded
		select {
		case msg.Result <- fmt.Errorf("max retries exceeded: %w", err):
		default:
		}
	}
}

// healthCheckLoop monitors connection health with actual verification
func (c *Client) healthCheckLoop() {
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

			// Actually verify the connection is alive
			if !c.verifyConnection() {
				consecutiveFails++
				c.healthCheckFails.Add(1)
				logging.Warn("Health check failed (%d/%d)", consecutiveFails, maxConsecutiveFails)

				if consecutiveFails >= maxConsecutiveFails {
					logging.Error("Health check failed %d times, triggering reconnect", consecutiveFails)
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

// verifyConnection checks if the connection is actually working
func (c *Client) verifyConnection() bool {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return false
	}

	// Set a short deadline and try to detect if connection is alive
	// We use SetReadDeadline with a very short timeout to check for errors
	conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))

	// Try to read - we expect timeout (connection alive) or error (connection dead)
	one := make([]byte, 1)
	_, err := conn.Read(one)

	// Reset deadline
	conn.SetReadDeadline(time.Time{})

	if err != nil {
		// Timeout is expected and means connection is alive
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return true
		}
		// Any other error means connection is dead
		return false
	}

	// We got data - unexpected but connection is alive
	// Note: This could mess up protocol framing, but health check
	// shouldn't receive data in normal operation
	return true
}

// Send sends a message asynchronously (non-blocking)
func (c *Client) Send(msgType byte, payload interface{}) error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	_, err := c.sendQueue.Enqueue(c.ctx, msgType, payload)
	return err
}

// SendSync sends a message and waits for completion
func (c *Client) SendSync(msgType byte, payload interface{}) error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	return c.sendQueue.EnqueueSync(c.ctx, msgType, payload)
}

// SendAndReceive sends a message and waits for a response
func (c *Client) SendAndReceive(msgType byte, payload interface{}) (byte, []byte, error) {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return 0, nil, ErrShuttingDown
	}
	c.shutdownMu.Unlock()

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return 0, nil, ErrNotConnected
	}

	// Set deadlines
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	c.conn.SetReadDeadline(time.Now().Add(c.config.AckTimeout))

	if err := c.encoder.Encode(msgType, payload); err != nil {
		return 0, nil, fmt.Errorf("send: %w", err)
	}

	if err := c.bufW.Flush(); err != nil {
		return 0, nil, fmt.Errorf("flush: %w", err)
	}

	respType, respData, err := c.decoder.Decode()
	if err != nil {
		// Connection error - trigger reconnect
		go c.triggerReconnect()
		return 0, nil, fmt.Errorf("receive: %w", err)
	}

	c.messagesAcked.Add(1)
	return respType, respData, nil
}

// State returns the current connection state
func (c *Client) State() ConnectionState {
	return ConnectionState(c.state.Load())
}

// Stats returns client statistics
func (c *Client) Stats() map[string]uint64 {
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

// Close closes the connection with graceful shutdown
func (c *Client) Close() error {
	c.shutdownMu.Lock()
	if c.shutdown {
		c.shutdownMu.Unlock()
		return nil
	}
	c.shutdown = true
	c.shutdownMu.Unlock()

	logging.Info("Client shutting down...")

	// Signal goroutines to stop
	c.cancel()

	// Close queue to unblock sendLoop
	c.sendQueue.Close()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logging.Info("Client goroutines stopped gracefully")
	case <-time.After(c.config.ShutdownTimeout):
		logging.Warn("Client shutdown timed out after %v", c.config.ShutdownTimeout)
	}

	// Close connection
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.state.Store(int32(StateDisconnected))
		return err
	}
	c.state.Store(int32(StateDisconnected))
	return nil
}
