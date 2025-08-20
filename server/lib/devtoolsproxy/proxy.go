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
func WebSocketProxyHandler(mgr *UpstreamManager, logger *slog.Logger) http.Handler {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
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
		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("websocket upgrade failed", slog.String("err", err.Error()))
			return
		}
		upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL, nil)
		if err != nil {
			logger.Error("dial upstream failed", slog.String("err", err.Error()), slog.String("url", upstreamURL))
			_ = clientConn.Close()
			return
		}
		logger.Debug("proxying devtools websocket", slog.String("url", upstreamURL))

		var once sync.Once
		cleanup := func() {
			once.Do(func() {
				_ = upstreamConn.Close()
				_ = clientConn.Close()
			})
		}
		proxyWebSocket(r.Context(), clientConn, upstreamConn, cleanup, logger)
	})
}

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

func proxyWebSocket(ctx context.Context, clientConn, upstreamConn wsConn, onClose func(), logger *slog.Logger) {
	errChan := make(chan error, 2)

	// Single-writer guarantee for client connection
	var clientWriteMu sync.Mutex

	// Heartbeat tracking
	var hbMu sync.Mutex
	lastClientActivity := time.Now()
	var lastPingSent time.Time
	var lastPongReceived time.Time
	var outstandingPing bool

	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				break
			}
			// Record any client activity
			hbMu.Lock()
			lastClientActivity = time.Now()
			hbMu.Unlock()

			// Handle control frames from client
			if mt == websocket.PongMessage {
				hbMu.Lock()
				lastPongReceived = time.Now()
				outstandingPing = false
				hbMu.Unlock()
				continue
			}
			if mt == websocket.PingMessage {
				clientWriteMu.Lock()
				_ = clientConn.WriteMessage(websocket.PongMessage, nil)
				clientWriteMu.Unlock()
				continue
			}

			if err := upstreamConn.WriteMessage(mt, msg); err != nil {
				errChan <- err
				break
			}
		}
	}()
	go func() {
		for {
			mt, msg, err := upstreamConn.ReadMessage()
			if err != nil {
				errChan <- err
				break
			}
			clientWriteMu.Lock()
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				clientWriteMu.Unlock()
				errChan <- err
				break
			}
			clientWriteMu.Unlock()
		}
	}()

	// Heartbeat goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				hbMu.Lock()
				inactivity := now.Sub(lastClientActivity)
				pingOutstanding := outstandingPing
				lastPing := lastPingSent
				lastPong := lastPongReceived
				hbMu.Unlock()

				if pingOutstanding {
					if now.Sub(lastPing) > 10*time.Second && lastPong.Before(lastPing) {
						logger.Warn("client ping timeout; closing devtools websocket")
						select {
						case errChan <- fmt.Errorf("ping timeout"):
						default:
						}
						return
					}
					continue
				}

				if inactivity >= 30*time.Second {
					clientWriteMu.Lock()
					pingErr := clientConn.WriteMessage(websocket.PingMessage, nil)
					clientWriteMu.Unlock()
					if pingErr != nil {
						select {
						case errChan <- pingErr:
						default:
						}
						return
					}
					hbMu.Lock()
					lastPingSent = now
					outstandingPing = true
					hbMu.Unlock()
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-errChan:
	}
	onClose()
}
