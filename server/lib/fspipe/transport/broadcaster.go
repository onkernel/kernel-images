// Package transport provides the broadcaster - a WebSocket server that external clients connect to.
// When the FUSE daemon writes, it broadcasts to all connected clients.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
)

const (
	// Timeouts
	writeTimeout    = 10 * time.Second
	ackTimeout      = 15 * time.Second
	pingInterval    = 30 * time.Second
	pongTimeout     = 10 * time.Second
	shutdownTimeout = 5 * time.Second

	// Buffer sizes
	responseChSize  = 100
	writeBufferSize = 256 * 1024
	readBufferSize  = 64 * 1024
)

// clientConn wraps a WebSocket connection with health tracking
type clientConn struct {
	conn       *websocket.Conn
	responseCh chan wsResponse
	addr       string
	healthy    atomic.Bool
	lastPong   atomic.Int64
	writeMu    sync.Mutex // Per-connection write lock
}

func newClientConn(conn *websocket.Conn) *clientConn {
	c := &clientConn{
		conn:       conn,
		responseCh: make(chan wsResponse, responseChSize),
		addr:       conn.RemoteAddr().String(),
	}
	c.healthy.Store(true)
	c.lastPong.Store(time.Now().UnixNano())
	return c
}

func (c *clientConn) isHealthy() bool {
	if !c.healthy.Load() {
		return false
	}
	// Check if we've received a pong recently
	lastPong := time.Unix(0, c.lastPong.Load())
	return time.Since(lastPong) < pingInterval+pongTimeout
}

func (c *clientConn) writeWithDeadline(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	err := c.conn.WriteMessage(websocket.BinaryMessage, data)
	c.conn.SetWriteDeadline(time.Time{}) // Clear deadline

	if err != nil {
		c.healthy.Store(false)
	}
	return err
}

func (c *clientConn) ping() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	err := c.conn.WriteMessage(websocket.PingMessage, []byte{})
	c.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		c.healthy.Store(false)
	}
	return err
}

// Broadcaster is a WebSocket server that broadcasts file ops to connected clients.
// External clients connect to receive file chunks and operations.
type Broadcaster struct {
	addr   string
	path   string
	server *http.Server

	mu      sync.RWMutex
	clients map[*websocket.Conn]*clientConn
	state   ConnectionState

	// Per-file request tracking for concurrent file operations
	fileMu    sync.RWMutex
	fileReqs  map[string]*fileRequest // fileID -> pending request

	// Require at least one client for writes (fail-safe mode)
	requireClient atomic.Bool

	// Fast mode: don't wait for ACKs on writes (fire-and-forget)
	// Only FileCreate waits for ACK, writes are async
	fastMode atomic.Bool

	// Stats
	messagesSent   atomic.Uint64
	messagesRecv   atomic.Uint64
	bytesSent      atomic.Uint64
	bytesRecv      atomic.Uint64
	clientsTotal   atomic.Uint64
	clientsCurrent atomic.Int64
	errors         atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc

	upgrader websocket.Upgrader
}

// fileRequest tracks a pending request for a specific file
type fileRequest struct {
	mu       sync.Mutex
	waiting  bool
	respCh   chan wsResponse
	deadline time.Time
}

// NewBroadcaster creates a new broadcaster that listens on the given address.
// Clients connect to ws://addr/path to receive file operations.
func NewBroadcaster(addr, path string) *Broadcaster {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Broadcaster{
		addr:     addr,
		path:     path,
		clients:  make(map[*websocket.Conn]*clientConn),
		fileReqs: make(map[string]*fileRequest),
		state:    StateDisconnected,
		ctx:      ctx,
		cancel:   cancel,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  readBufferSize,
			WriteBufferSize: writeBufferSize,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	// Default: require at least one client (fail-safe)
	b.requireClient.Store(true)
	return b
}

// SetRequireClient sets whether writes should fail when no clients are connected.
// If true (default), writes fail with error when no clients. If false, fake ACKs are returned.
func (b *Broadcaster) SetRequireClient(require bool) {
	b.requireClient.Store(require)
}

// SetFastMode enables fire-and-forget mode for write operations.
// In fast mode, only FileCreate waits for ACK. Writes are sent async without waiting.
// This significantly improves throughput but trades off guaranteed delivery.
func (b *Broadcaster) SetFastMode(fast bool) {
	b.fastMode.Store(fast)
}

// Connect starts the WebSocket server.
func (b *Broadcaster) Connect() error {
	mux := http.NewServeMux()
	mux.HandleFunc(b.path, b.handleWebSocket)

	b.server = &http.Server{
		Addr:    b.addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)

	// Start server in background
	go func() {
		logging.Info("Broadcaster listening on %s%s", b.addr, b.path)
		if err := b.server.ListenAndServe(); err != http.ErrServerClosed {
			logging.Error("Broadcaster server error: %v", err)
			errCh <- err
		}
	}()

	// Wait a bit and check for immediate errors
	select {
	case err := <-errCh:
		return fmt.Errorf("broadcaster failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
	}

	b.mu.Lock()
	b.state = StateConnected
	b.mu.Unlock()

	// Start health monitor
	go b.healthMonitor()

	return nil
}

// healthMonitor periodically pings clients and removes dead ones
func (b *Broadcaster) healthMonitor() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.pingClients()
			b.removeDeadClients()
		}
	}
}

func (b *Broadcaster) pingClients() {
	b.mu.RLock()
	clients := make([]*clientConn, 0, len(b.clients))
	for _, c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	for _, c := range clients {
		if err := c.ping(); err != nil {
			logging.Debug("Ping failed for %s: %v", c.addr, err)
		}
	}
}

func (b *Broadcaster) removeDeadClients() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for conn, c := range b.clients {
		if !c.isHealthy() {
			logging.Info("Removing dead client: %s", c.addr)
			conn.Close()
			close(c.responseCh)
			delete(b.clients, conn)
			b.clientsCurrent.Add(-1)
		}
	}
}

func (b *Broadcaster) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Error("Broadcaster upgrade error: %v", err)
		return
	}

	client := newClientConn(conn)

	// Set up pong handler
	conn.SetPongHandler(func(string) error {
		client.lastPong.Store(time.Now().UnixNano())
		return nil
	})

	b.mu.Lock()
	b.clients[conn] = client
	b.mu.Unlock()

	b.clientsTotal.Add(1)
	b.clientsCurrent.Add(1)

	logging.Info("Client connected: %s (total: %d)", client.addr, b.clientsCurrent.Load())

	// Read responses from this client
	go b.readLoop(client)
}

func (b *Broadcaster) readLoop(client *clientConn) {
	defer func() {
		b.mu.Lock()
		delete(b.clients, client.conn)
		close(client.responseCh)
		b.mu.Unlock()

		b.clientsCurrent.Add(-1)
		client.conn.Close()
		client.healthy.Store(false)
		logging.Info("Client disconnected: %s (total: %d)", client.addr, b.clientsCurrent.Load())
	}()

	for {
		_, rawData, err := client.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logging.Debug("Client read error from %s: %v", client.addr, err)
			}
			return
		}

		b.messagesRecv.Add(1)
		b.bytesRecv.Add(uint64(len(rawData)))

		if len(rawData) < 5 {
			logging.Debug("Malformed message from %s: too short", client.addr)
			continue
		}

		msgType := rawData[4]
		msgData := rawData[5:]

		// Route ACK to the appropriate file request
		b.routeResponse(msgType, msgData)
	}
}

// routeResponse routes an ACK response to the waiting file request
func (b *Broadcaster) routeResponse(msgType byte, data []byte) {
	// Extract file_id from response
	var resp struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		logging.Debug("Failed to parse response file_id: %v", err)
		return
	}

	b.fileMu.RLock()
	req, ok := b.fileReqs[resp.FileID]
	b.fileMu.RUnlock()

	if !ok || req == nil {
		logging.Debug("No pending request for file %s", resp.FileID)
		return
	}

	req.mu.Lock()
	if req.waiting {
		select {
		case req.respCh <- wsResponse{msgType: msgType, data: data}:
		default:
			logging.Debug("Response channel full for file %s", resp.FileID)
		}
	}
	req.mu.Unlock()
}

// getHealthyClients returns a list of healthy connected clients
func (b *Broadcaster) getHealthyClients() []*clientConn {
	b.mu.RLock()
	defer b.mu.RUnlock()

	clients := make([]*clientConn, 0, len(b.clients))
	for _, c := range b.clients {
		if c.isHealthy() {
			clients = append(clients, c)
		}
	}
	return clients
}

// Send broadcasts a message to all connected clients (fire-and-forget).
func (b *Broadcaster) Send(msgType byte, payload interface{}) error {
	encodedData, err := b.encodeMessage(msgType, payload)
	if err != nil {
		return err
	}

	clients := b.getHealthyClients()

	if len(clients) == 0 {
		if b.requireClient.Load() {
			b.errors.Add(1)
			return fmt.Errorf("no healthy clients connected")
		}
		logging.Debug("No clients connected, message dropped")
		return nil
	}

	var sendErrors int
	for _, c := range clients {
		if err := c.writeWithDeadline(encodedData); err != nil {
			logging.Debug("Broadcast write error to %s: %v", c.addr, err)
			sendErrors++
		}
	}

	// Fail if all sends failed
	if sendErrors == len(clients) {
		b.errors.Add(1)
		return fmt.Errorf("failed to send to all %d clients", len(clients))
	}

	b.messagesSent.Add(1)
	b.bytesSent.Add(uint64(len(encodedData)))

	return nil
}

// SendSync sends a message and waits for flush (broadcasts to all clients).
func (b *Broadcaster) SendSync(msgType byte, payload interface{}) error {
	return b.Send(msgType, payload)
}

// SendAndReceive broadcasts a message and waits for ACK from any client.
func (b *Broadcaster) SendAndReceive(msgType byte, payload interface{}) (byte, []byte, error) {
	// Fast mode: fire-and-forget for writes, only wait for FileCreate ACK
	if b.fastMode.Load() && msgType == protocol.MsgWriteChunk {
		msg := payload.(*protocol.WriteChunk)
		if err := b.Send(msgType, payload); err != nil {
			return 0, nil, err
		}
		// Return immediate fake ACK
		ack := protocol.WriteAck{FileID: msg.FileID, Offset: msg.Offset, Written: len(msg.Data)}
		data, _ := json.Marshal(ack)
		return protocol.MsgWriteAck, data, nil
	}

	// Extract file ID for routing
	var fileID string
	switch msg := payload.(type) {
	case *protocol.FileCreate:
		fileID = msg.FileID
	case *protocol.WriteChunk:
		fileID = msg.FileID
	default:
		// For other message types, use a random ID
		fileID = fmt.Sprintf("_req_%d", time.Now().UnixNano())
	}

	// Create or get file request tracker
	b.fileMu.Lock()
	req, ok := b.fileReqs[fileID]
	if !ok {
		req = &fileRequest{
			respCh: make(chan wsResponse, 1),
		}
		b.fileReqs[fileID] = req
	}
	b.fileMu.Unlock()

	// Mark as waiting
	req.mu.Lock()
	req.waiting = true
	req.deadline = time.Now().Add(ackTimeout)
	// Drain any stale responses
	select {
	case <-req.respCh:
	default:
	}
	req.mu.Unlock()

	// Cleanup when done
	defer func() {
		req.mu.Lock()
		req.waiting = false
		req.mu.Unlock()
	}()

	// Encode and send
	encodedData, err := b.encodeMessage(msgType, payload)
	if err != nil {
		return 0, nil, err
	}

	clients := b.getHealthyClients()

	if len(clients) == 0 {
		if b.requireClient.Load() {
			b.errors.Add(1)
			return 0, nil, fmt.Errorf("no healthy clients connected")
		}
		// Fallback to fake ACK if not requiring clients
		return b.fakeAck(msgType, payload)
	}

	// Send to all healthy clients
	var sendErrors int
	for _, c := range clients {
		if err := c.writeWithDeadline(encodedData); err != nil {
			logging.Debug("Broadcast write error to %s: %v", c.addr, err)
			sendErrors++
		}
	}

	if sendErrors == len(clients) {
		b.errors.Add(1)
		return 0, nil, fmt.Errorf("failed to send to all %d clients", len(clients))
	}

	b.messagesSent.Add(1)
	b.bytesSent.Add(uint64(len(encodedData)))

	// Wait for ACK
	select {
	case resp := <-req.respCh:
		return resp.msgType, resp.data, nil
	case <-time.After(ackTimeout):
		b.errors.Add(1)
		return 0, nil, fmt.Errorf("ACK timeout after %v for file %s", ackTimeout, fileID)
	case <-b.ctx.Done():
		return 0, nil, fmt.Errorf("broadcaster shutting down")
	}
}

// fakeAck returns a fake ACK when no clients are connected (only if requireClient is false)
func (b *Broadcaster) fakeAck(msgType byte, payload interface{}) (byte, []byte, error) {
	switch msgType {
	case protocol.MsgFileCreate:
		msg := payload.(*protocol.FileCreate)
		ack := protocol.FileCreateAck{FileID: msg.FileID, Success: true}
		data, _ := json.Marshal(ack)
		logging.Debug("Fake ACK for FileCreate %s (no clients)", msg.FileID)
		return protocol.MsgFileCreateAck, data, nil
	case protocol.MsgWriteChunk:
		msg := payload.(*protocol.WriteChunk)
		ack := protocol.WriteAck{FileID: msg.FileID, Offset: msg.Offset, Written: len(msg.Data)}
		data, _ := json.Marshal(ack)
		return protocol.MsgWriteAck, data, nil
	default:
		return 0, nil, nil
	}
}

func (b *Broadcaster) encodeMessage(msgType byte, payload interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	totalLen := 1 + len(jsonData)
	data := make([]byte, 4+totalLen)

	data[0] = byte(totalLen >> 24)
	data[1] = byte(totalLen >> 16)
	data[2] = byte(totalLen >> 8)
	data[3] = byte(totalLen)
	data[4] = msgType
	copy(data[5:], jsonData)

	return data, nil
}

// State returns the current connection state.
func (b *Broadcaster) State() ConnectionState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// ClientCount returns the number of healthy connected clients.
func (b *Broadcaster) ClientCount() int {
	return len(b.getHealthyClients())
}

// Stats returns broadcaster statistics.
func (b *Broadcaster) Stats() map[string]uint64 {
	return map[string]uint64{
		"messages_sent":   b.messagesSent.Load(),
		"messages_recv":   b.messagesRecv.Load(),
		"bytes_sent":      b.bytesSent.Load(),
		"bytes_recv":      b.bytesRecv.Load(),
		"clients_total":   b.clientsTotal.Load(),
		"clients_current": uint64(b.clientsCurrent.Load()),
		"errors":          b.errors.Load(),
	}
}

// Close shuts down the broadcaster gracefully.
func (b *Broadcaster) Close() error {
	b.cancel() // Signal shutdown

	b.mu.Lock()
	b.state = StateDisconnected

	// Close all client connections gracefully
	for conn, c := range b.clients {
		// Send close message
		c.writeMu.Lock()
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
			time.Now().Add(time.Second),
		)
		c.writeMu.Unlock()
		conn.Close()
		close(c.responseCh)
	}
	b.clients = make(map[*websocket.Conn]*clientConn)
	b.mu.Unlock()

	// Shutdown HTTP server
	if b.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return b.server.Shutdown(ctx)
	}
	return nil
}
