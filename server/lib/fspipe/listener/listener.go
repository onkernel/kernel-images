// Package listener provides the fspipe listener server functionality.
// The listener receives file operations from fspipe daemons and writes files locally.
package listener

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
)

// Server is the TCP/WebSocket server that receives file operations
type Server struct {
	addr       string
	localDir   string
	listener   net.Listener
	httpServer *http.Server

	wsEnabled bool
	wsPath    string
	upgrader  websocket.Upgrader

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Shutdown management
	shutdownTimeout time.Duration

	// Metrics
	activeConnections atomic.Int64
	totalConnections  atomic.Uint64
	totalFiles        atomic.Uint64
	totalBytes        atomic.Uint64
	totalErrors       atomic.Uint64
}

// Config holds server configuration
type Config struct {
	WebSocketEnabled bool
	WebSocketPath    string
	ShutdownTimeout  time.Duration
}

// NewServer creates a new listener server (TCP mode)
func NewServer(addr string, localDir string) *Server {
	return NewServerWithConfig(addr, localDir, Config{})
}

// NewServerWithConfig creates a new listener server with configuration
func NewServerWithConfig(addr string, localDir string, config Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 10 * time.Second
	}

	s := &Server{
		addr:            addr,
		localDir:        localDir,
		wsEnabled:       config.WebSocketEnabled,
		wsPath:          config.WebSocketPath,
		ctx:             ctx,
		cancel:          cancel,
		shutdownTimeout: shutdownTimeout,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  64 * 1024,
			WriteBufferSize: 64 * 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	if s.wsPath == "" {
		s.wsPath = "/fspipe"
	}

	return s
}

// Start begins listening for connections
func (s *Server) Start() error {
	if s.wsEnabled {
		return s.startWebSocket()
	}
	return s.startTCP()
}

func (s *Server) startTCP() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln

	logging.Info("TCP listening on %s, writing files to %s", s.addr, s.localDir)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

func (s *Server) startWebSocket() error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.wsPath, s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln

	logging.Info("WebSocket listening on %s%s, writing files to %s", s.addr, s.wsPath, s.localDir)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(ln); err != http.ErrServerClosed {
			logging.Error("HTTP server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	// Exponential backoff for accept errors
	backoff := 10 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				logging.Error("Accept error: %v", err)
				s.totalErrors.Add(1)

				// Backoff to prevent CPU spin on persistent errors
				timer := time.NewTimer(backoff)
				select {
				case <-s.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}

				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		// Reset backoff on successful accept
		backoff = 10 * time.Millisecond

		s.totalConnections.Add(1)
		s.activeConnections.Add(1)
		logging.Info("New TCP connection from %s", conn.RemoteAddr())

		s.wg.Add(1)
		go s.handleTCPConnection(conn)
	}
}

func (s *Server) handleTCPConnection(conn net.Conn) {
	defer s.wg.Done()
	defer s.activeConnections.Add(-1)
	defer conn.Close()

	handler := newHandler(s.localDir, &s.totalFiles, &s.totalBytes, &s.totalErrors)
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	handler.handle(s.ctx, reader, writer)

	logging.Info("TCP connection from %s closed", conn.RemoteAddr())
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Error("WebSocket upgrade error: %v", err)
		s.totalErrors.Add(1)
		return
	}

	s.totalConnections.Add(1)
	s.activeConnections.Add(1)
	logging.Info("New WebSocket connection from %s", r.RemoteAddr)

	s.wg.Add(1)
	go s.handleWSConnection(conn, r.RemoteAddr)
}

func (s *Server) handleWSConnection(conn *websocket.Conn, remoteAddr string) {
	defer s.wg.Done()
	defer s.activeConnections.Add(-1)
	defer conn.Close()

	handler := newHandler(s.localDir, &s.totalFiles, &s.totalBytes, &s.totalErrors)
	wsAdapter := newWebSocketAdapter(conn)

	handler.handle(s.ctx, wsAdapter, wsAdapter)

	logging.Info("WebSocket connection from %s closed", remoteAddr)
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	logging.Info("Server shutting down...")
	s.cancel()

	// Shutdown HTTP server with timeout
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}

	if s.listener != nil {
		s.listener.Close()
	}

	// Wait for connections with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logging.Info("Server stopped gracefully")
	case <-time.After(s.shutdownTimeout):
		logging.Warn("Server shutdown timed out after %v", s.shutdownTimeout)
	}

	return nil
}

// Addr returns the listener address
func (s *Server) Addr() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// LocalDir returns the local directory where files are written
func (s *Server) LocalDir() string {
	return s.localDir
}

// WSPath returns the WebSocket path
func (s *Server) WSPath() string {
	return s.wsPath
}

// Stats returns server statistics
func (s *Server) Stats() map[string]interface{} {
	return map[string]interface{}{
		"active_connections": s.activeConnections.Load(),
		"total_connections":  s.totalConnections.Load(),
		"total_files":        s.totalFiles.Load(),
		"total_bytes":        s.totalBytes.Load(),
		"total_errors":       s.totalErrors.Load(),
	}
}

// flusher is an interface for types that can flush buffered data
type flusher interface {
	io.Writer
	Flush() error
}

// handler processes incoming messages and manages local files
type handler struct {
	localDir string

	mu    sync.RWMutex
	files map[string]*fileEntry

	// Shared metrics
	totalFiles  *atomic.Uint64
	totalBytes  *atomic.Uint64
	totalErrors *atomic.Uint64
}

// fileEntry wraps a file with metadata for tracking
type fileEntry struct {
	file      *os.File
	path      string
	createdAt time.Time
	bytesW    int64
}

func newHandler(localDir string, totalFiles, totalBytes, totalErrors *atomic.Uint64) *handler {
	return &handler{
		localDir:    localDir,
		files:       make(map[string]*fileEntry),
		totalFiles:  totalFiles,
		totalBytes:  totalBytes,
		totalErrors: totalErrors,
	}
}

func (h *handler) handle(ctx context.Context, r io.Reader, w flusher) {
	// Panic recovery to prevent one bad message from crashing the server
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Handler panic recovered: %v", r)
			if h.totalErrors != nil {
				h.totalErrors.Add(1)
			}
		}
		h.closeAllFiles()
	}()

	decoder := protocol.NewDecoder(r)
	encoder := protocol.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgType, payload, err := decoder.Decode()
		if err != nil {
			if err != io.EOF {
				logging.Debug("Decode error: %v", err)
			}
			return
		}

		if err := h.handleMessage(msgType, payload, encoder, w); err != nil {
			logging.Debug("Handle message error: %v", err)
			if h.totalErrors != nil {
				h.totalErrors.Add(1)
			}
		}
	}
}

func (h *handler) handleMessage(msgType byte, payload []byte, encoder *protocol.Encoder, w flusher) error {
	switch msgType {
	case protocol.MsgFileCreate:
		var msg protocol.FileCreate
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleFileCreate(&msg, encoder, w)

	case protocol.MsgFileClose:
		var msg protocol.FileClose
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleFileClose(&msg)

	case protocol.MsgWriteChunk:
		var msg protocol.WriteChunk
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleWriteChunk(&msg, encoder, w)

	case protocol.MsgTruncate:
		var msg protocol.Truncate
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleTruncate(&msg)

	case protocol.MsgRename:
		var msg protocol.Rename
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleRename(&msg)

	case protocol.MsgDelete:
		var msg protocol.Delete
		if err := protocol.DecodePayload(payload, &msg); err != nil {
			return err
		}
		return h.handleDelete(&msg)

	default:
		logging.Debug("Unknown message type: 0x%02x", msgType)
		return nil
	}
}

func (h *handler) handleFileCreate(msg *protocol.FileCreate, encoder *protocol.Encoder, w flusher) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	path := filepath.Join(h.localDir, msg.Filename)

	ack := protocol.FileCreateAck{
		FileID:  msg.FileID,
		Success: true,
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		ack.Success = false
		ack.Error = err.Error()
		if encErr := encoder.Encode(protocol.MsgFileCreateAck, &ack); encErr != nil {
			return encErr
		}
		return w.Flush()
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(msg.Mode))
	if err != nil {
		ack.Success = false
		ack.Error = err.Error()
		if encErr := encoder.Encode(protocol.MsgFileCreateAck, &ack); encErr != nil {
			return encErr
		}
		return w.Flush()
	}

	h.files[msg.FileID] = &fileEntry{
		file:      f,
		path:      path,
		createdAt: time.Now(),
	}

	if h.totalFiles != nil {
		h.totalFiles.Add(1)
	}

	logging.Debug("Created file: %s (id=%s)", msg.Filename, msg.FileID)

	// Send success ACK
	if err := encoder.Encode(protocol.MsgFileCreateAck, &ack); err != nil {
		return err
	}
	return w.Flush()
}

func (h *handler) handleFileClose(msg *protocol.FileClose) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.files[msg.FileID]
	if !ok {
		logging.Debug("FileClose: unknown file ID %s", msg.FileID)
		return nil
	}

	// Sync before close to ensure data is written
	if err := entry.file.Sync(); err != nil {
		logging.Debug("Sync error for %s: %v", msg.FileID, err)
	}

	if err := entry.file.Close(); err != nil {
		logging.Debug("Close error for %s: %v", msg.FileID, err)
	}

	delete(h.files, msg.FileID)
	logging.Debug("Closed file: id=%s, bytes=%d, duration=%v",
		msg.FileID, entry.bytesW, time.Since(entry.createdAt))
	return nil
}

func (h *handler) handleWriteChunk(msg *protocol.WriteChunk, encoder *protocol.Encoder, w flusher) error {
	h.mu.RLock()
	entry, ok := h.files[msg.FileID]
	h.mu.RUnlock()

	ack := protocol.WriteAck{
		FileID: msg.FileID,
		Offset: msg.Offset,
	}

	if !ok {
		ack.Error = "unknown file ID"
		if err := encoder.Encode(protocol.MsgWriteAck, &ack); err != nil {
			return err
		}
		return w.Flush()
	}

	n, err := entry.file.WriteAt(msg.Data, msg.Offset)
	if err != nil {
		ack.Error = err.Error()
	} else {
		// Track bytes written
		h.mu.Lock()
		entry.bytesW += int64(n)
		h.mu.Unlock()

		if h.totalBytes != nil {
			h.totalBytes.Add(uint64(n))
		}
	}
	ack.Written = n

	if err := encoder.Encode(protocol.MsgWriteAck, &ack); err != nil {
		return err
	}
	return w.Flush()
}

func (h *handler) handleTruncate(msg *protocol.Truncate) error {
	h.mu.RLock()
	entry, ok := h.files[msg.FileID]
	h.mu.RUnlock()

	if !ok {
		logging.Debug("Truncate: unknown file ID %s", msg.FileID)
		return nil
	}

	if err := entry.file.Truncate(msg.Size); err != nil {
		logging.Debug("Truncate error for %s: %v", msg.FileID, err)
		return err
	}

	logging.Debug("Truncated file: id=%s to %d bytes", msg.FileID, msg.Size)
	return nil
}

func (h *handler) handleRename(msg *protocol.Rename) error {
	oldPath := filepath.Join(h.localDir, msg.OldName)
	newPath := filepath.Join(h.localDir, msg.NewName)

	dir := filepath.Dir(newPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		logging.Debug("Rename error: %v", err)
		return err
	}

	logging.Debug("Renamed: %s -> %s", msg.OldName, msg.NewName)
	return nil
}

func (h *handler) handleDelete(msg *protocol.Delete) error {
	path := filepath.Join(h.localDir, msg.Filename)

	if err := os.Remove(path); err != nil {
		logging.Debug("Delete error: %v", err)
		return err
	}

	logging.Debug("Deleted: %s", msg.Filename)
	return nil
}

func (h *handler) closeAllFiles() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, entry := range h.files {
		if err := entry.file.Sync(); err != nil {
			logging.Debug("Sync error during cleanup for %s: %v", id, err)
		}
		if err := entry.file.Close(); err != nil {
			logging.Debug("Close error during cleanup for %s: %v", id, err)
		}
		delete(h.files, id)
	}
}

// webSocketAdapter adapts a WebSocket connection to io.Reader/Writer interfaces
type webSocketAdapter struct {
	conn *websocket.Conn

	readMu  sync.Mutex
	readBuf bytes.Buffer

	writeMu  sync.Mutex
	writeBuf bytes.Buffer
}

func newWebSocketAdapter(conn *websocket.Conn) *webSocketAdapter {
	return &webSocketAdapter{
		conn: conn,
	}
}

func (a *webSocketAdapter) Read(p []byte) (int, error) {
	a.readMu.Lock()
	defer a.readMu.Unlock()

	if a.readBuf.Len() > 0 {
		return a.readBuf.Read(p)
	}

	messageType, data, err := a.conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	if messageType != websocket.BinaryMessage {
		return a.Read(p)
	}

	a.readBuf.Write(data)
	return a.readBuf.Read(p)
}

func (a *webSocketAdapter) Write(p []byte) (int, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()

	return a.writeBuf.Write(p)
}

func (a *webSocketAdapter) Flush() error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()

	if a.writeBuf.Len() == 0 {
		return nil
	}

	data := a.writeBuf.Bytes()
	a.writeBuf.Reset()

	if err := a.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		logging.Error("WebSocket write error: %v", err)
		return err
	}

	return nil
}

func (a *webSocketAdapter) Close() error {
	return a.conn.Close()
}

var _ io.Reader = (*webSocketAdapter)(nil)
var _ io.Writer = (*webSocketAdapter)(nil)
