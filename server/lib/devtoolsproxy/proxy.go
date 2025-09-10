package devtoolsproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var devtoolsListeningRegexp = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

// UpstreamManager tails the Chromium supervisord log and extracts the current DevTools
// websocket URL, updating it whenever Chromium restarts and emits a new line.
type UpstreamManager struct {
	logFilePath string
	logger      *slog.Logger

	currentURL atomic.Value // string

	startOnce  sync.Once
	stopOnce   sync.Once
	cancelTail context.CancelFunc
}

func NewUpstreamManager(logFilePath string, logger *slog.Logger) *UpstreamManager {
	um := &UpstreamManager{logFilePath: logFilePath, logger: logger}
	um.currentURL.Store("")
	return um
}

// Start begins background tailing and updating the upstream URL until ctx is done.
func (u *UpstreamManager) Start(ctx context.Context) {
	u.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		u.cancelTail = cancel
		go u.tailLoop(ctx)
	})
}

// Stop cancels the background tailer.
func (u *UpstreamManager) Stop() {
	u.stopOnce.Do(func() {
		if u.cancelTail != nil {
			u.cancelTail()
		}
	})
}

// WaitForInitial blocks until an initial upstream URL has been discovered or the timeout elapses.
func (u *UpstreamManager) WaitForInitial(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if url := u.Current(); url != "" {
			return url, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("devtools upstream not found within %s", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Current returns the current upstream websocket URL if known, or empty string.
func (u *UpstreamManager) Current() string {
	val, _ := u.currentURL.Load().(string)
	return val
}

func (u *UpstreamManager) setCurrent(url string) {
	prev := u.Current()
	if url != "" && url != prev {
		u.logger.Info("devtools upstream updated", slog.String("url", url))
		u.currentURL.Store(url)
	}
}

func (u *UpstreamManager) tailLoop(ctx context.Context) {
	backoff := 250 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return
		}
		// Run one tail session. If it exits, retry with a small backoff.
		u.runTailOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		// cap backoff to 2s
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

func (u *UpstreamManager) runTailOnce(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "tail", "-f", "-n", "+1", u.logFilePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		u.logger.Error("failed to open tail stdout", slog.String("err", err.Error()))
		return
	}
	if err := cmd.Start(); err != nil {
		// Common when file does not exist yet; log at debug level
		if strings.Contains(err.Error(), "No such file or directory") {
			u.logger.Debug("supervisord log not found yet; will retry", slog.String("path", u.logFilePath))
		} else {
			u.logger.Error("failed to start tail", slog.String("err", err.Error()))
		}
		return
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if matches := devtoolsListeningRegexp.FindStringSubmatch(line); len(matches) == 2 {
			u.setCurrent(matches[1])
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		u.logger.Error("tail scanner error", slog.String("err", err.Error()))
	}
}

// WebSocketProxyHandler returns an http.Handler that upgrades incoming connections and
// proxies them to the current upstream websocket URL. It expects only websocket requests.
// If logCDPMessages is true, all CDP messages will be logged with their direction.
func WebSocketProxyHandler(mgr *UpstreamManager, logger *slog.Logger, logCDPMessages bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCurrent := mgr.Current()
		if upstreamCurrent == "" {
			http.Error(w, "upstream not ready", http.StatusServiceUnavailable)
			return
		}
		parsed, err := url.Parse(upstreamCurrent)
		if err != nil {
			http.Error(w, "invalid upstream", http.StatusInternalServerError)
			return
		}
		// Always use the full upstream path and query, ignoring the client's request path/query
		upstreamURL := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path, RawQuery: parsed.RawQuery}).String()
		upgrader := websocket.Upgrader{
			ReadBufferSize:    65536,
			WriteBufferSize:   65536,
			EnableCompression: true,
			CheckOrigin:       func(r *http.Request) bool { return true },
		}
		logger.Info("upgrader config", slog.Any("upgrader", upgrader))
		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("websocket upgrade failed", slog.String("err", err.Error()))
			return
		}
		clientConn.SetReadDeadline(time.Time{})    // No timeout--hold on to connections for dear life
		clientConn.SetWriteDeadline(time.Time{})   // No timeout--hold on to connections for dear life
		clientConn.SetReadLimit(100 * 1024 * 1024) // 100 MB. Effectively no maximum size of message from client
		clientConn.EnableWriteCompression(true)
		clientConn.SetCompressionLevel(6)

		dialer := websocket.Dialer{
			ReadBufferSize:   65536,
			WriteBufferSize:  65536,
			HandshakeTimeout: 30 * time.Second,
		}
		logger.Info("dialer config", slog.Any("dialer", dialer))
		upstreamConn, _, err := dialer.Dial(upstreamURL, nil)
		if err != nil {
			logger.Error("dial upstream failed", slog.String("err", err.Error()), slog.String("url", upstreamURL))
			_ = clientConn.Close()
			return
		}
		upstreamConn.SetReadLimit(100 * 1024 * 1024) // 100 MB. Effectively no maximum size of message from upstream
		upstreamConn.EnableWriteCompression(true)
		upstreamConn.SetCompressionLevel(6)
		upstreamConn.SetReadDeadline(time.Time{})  // no timeout
		upstreamConn.SetWriteDeadline(time.Time{}) // no timeout
		logger.Debug("proxying devtools websocket", slog.String("url", upstreamURL))

		var once sync.Once
		cleanup := func() {
			once.Do(func() {
				_ = upstreamConn.Close()
				_ = clientConn.Close()
			})
		}
		proxyWebSocket(r.Context(), clientConn, upstreamConn, cleanup, logger, logCDPMessages)
	})
}

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// logCDPMessage logs a CDP message with its direction if logging is enabled
func logCDPMessage(logger *slog.Logger, direction string, mt int, msg []byte) {
	if mt != websocket.TextMessage {
		return // Only log text messages (CDP messages)
	}

	// Extract fields using regex from raw message
	rawMsg := string(msg)

	// Regex patterns to match "key":"val" or "key": "val" for string values
	extractStringField := func(key string) string {
		pattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, key)
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(rawMsg)
		if len(matches) > 1 {
			return matches[1]
		}
		return ""
	}

	// Regex pattern to match "key": number for numeric id
	extractNumberField := func(key string) interface{} {
		pattern := fmt.Sprintf(`"%s"\s*:\s*(\d+)`, key)
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(rawMsg)
		if len(matches) > 1 {
			// Try to parse as int first
			if val, err := strconv.Atoi(matches[1]); err == nil {
				return val
			}
			// Fall back to float64
			if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
				return val
			}
		}
		return nil
	}

	// Extract fields using regex
	method := extractStringField("method")
	id := extractNumberField("id")
	sessionId := extractStringField("sessionId")
	targetId := extractStringField("targetId")
	frameId := extractStringField("frameId")

	// Build log attributes, only including non-empty values
	attrs := []slog.Attr{
		slog.String("dir", direction),
	}

	if sessionId != "" {
		attrs = append(attrs, slog.String("sessionId", sessionId))
	}
	if targetId != "" {
		attrs = append(attrs, slog.String("targetId", targetId))
	}
	if id != nil {
		attrs = append(attrs, slog.Any("id", id))
	}
	if frameId != "" {
		attrs = append(attrs, slog.String("frameId", frameId))
	}

	if method != "" {
		attrs = append(attrs, slog.String("method", method))
	}

	attrs = append(attrs, slog.Int("raw_length", len(msg)))

	// Convert attrs to individual slog.Attr arguments
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}

	logger.Info("cdp", args...)
}

func proxyWebSocket(ctx context.Context, clientConn, upstreamConn wsConn, onClose func(), logger *slog.Logger, logCDPMessages bool) {
	errChan := make(chan error, 2)

	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				logger.Error("client read error", slog.String("err", err.Error()))
				errChan <- err
				break
			}

			// Log CDP messages if enabled
			if logCDPMessages {
				logCDPMessage(logger, "->", mt, msg)
			}

			if err := upstreamConn.WriteMessage(mt, msg); err != nil {
				logger.Error("upstream write error", slog.String("err", err.Error()))
				errChan <- err
				break
			}
		}
	}()
	go func() {
		for {
			mt, msg, err := upstreamConn.ReadMessage()
			if err != nil {
				logger.Error("upstream read error", slog.String("err", err.Error()))
				errChan <- err
				break
			}

			// Log CDP messages if enabled
			if logCDPMessages {
				logCDPMessage(logger, "<-", mt, msg)
			}

			if err := clientConn.WriteMessage(mt, msg); err != nil {
				logger.Error("client write error", slog.String("err", err.Error()))
				errChan <- err
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-errChan:
	}
	onClose()
}
