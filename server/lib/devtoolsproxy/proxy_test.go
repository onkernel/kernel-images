package devtoolsproxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/onkernel/kernel-images/server/lib/scaletozero"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func findBrowserBinary() (string, error) {
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no chromium/chrome binary found")
}

func getFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

func TestWaitForInitialTimeoutWhenLogMissing(t *testing.T) {
	mgr := NewUpstreamManager("/tmp/not-a-real-file-hopefully", silentLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)
	if _, err := mgr.WaitForInitial(300 * time.Millisecond); err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestWebSocketProxyHandler_ProxiesEcho(t *testing.T) {
	// Start an echo websocket server as upstream
	echoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			t.Fatalf("accept failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		for {
			mt, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			// echo back with path+query prefixed to verify preservation
			payload := []byte(r.URL.Path + "?" + r.URL.RawQuery + "|" + string(msg))
			if err := c.Write(ctx, mt, payload); err != nil {
				return
			}
		}
	}))
	defer echoSrv.Close()

	// Build ws URL for upstream
	u, _ := url.Parse(echoSrv.URL)
	u.Scheme = "ws"
	u.Path = "/echo"
	u.RawQuery = "k=v"

	logger := silentLogger()
	mgr := NewUpstreamManager("/dev/null", logger)
	// seed current upstream to echo server including path/query (bypass tailing)
	mgr.setCurrent((&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}).String())

	proxy := WebSocketProxyHandler(mgr, logger, false, scaletozero.NewNoopController())
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	// Connect to proxy with the same path/query and verify echo
	pu, _ := url.Parse(proxySrv.URL)
	pu.Scheme = "ws"
	// Provide a different client path/query; proxy should ignore these
	pu.Path = "/client"
	pu.RawQuery = "x=y"

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, pu.String(), nil)
	if err != nil {
		t.Fatalf("dial proxy failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	msg := "hello"
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	expectedPrefix := u.Path + "?" + u.RawQuery + "|"
	if !strings.HasPrefix(string(resp), expectedPrefix) || !strings.HasSuffix(string(resp), msg) {
		t.Fatalf("unexpected echo: %q", string(resp))
	}
}

func TestUpstreamManagerDetectsChromiumAndRestart(t *testing.T) {
	browser, err := findBrowserBinary()
	if err != nil {
		t.Skip("chromium/chrome not installed in environment")
	}

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "chromium.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer logFile.Close()

	logger := silentLogger()
	mgr := NewUpstreamManager(logPath, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	startChromium := func(port int) (*exec.Cmd, error) {
		userDir := t.TempDir()
		args := []string{
			"--headless=new",
			"--remote-debugging-address=127.0.0.1",
			fmt.Sprintf("--remote-debugging-port=%d", port),
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-gpu",
			"--disable-software-rasterizer",
			"--disable-dev-shm-usage",
			"--no-sandbox",
			"--disable-setuid-sandbox",
			fmt.Sprintf("--user-data-dir=%s", userDir),
			"about:blank",
		}
		cmd := exec.Command(browser, args...)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd, nil
	}

	port1, err := getFreePort()
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	cmd1, err := startChromium(port1)
	if err != nil {
		t.Fatalf("start chromium 1: %v", err)
	}
	defer func() {
		_ = cmd1.Process.Kill()
		_, _ = cmd1.Process.Wait()
	}()

	// Wait for initial upstream containing port1
	ok := waitForCondition(20*time.Second, func() bool {
		u := mgr.Current()
		return strings.Contains(u, fmt.Sprintf(":%d/", port1))
	})
	if !ok {
		t.Fatalf("did not detect initial upstream for port %d; got: %q", port1, mgr.Current())
	}

	// Restart on a new port
	port2, err := getFreePort()
	if err != nil {
		t.Fatalf("get free port 2: %v", err)
	}
	_ = cmd1.Process.Kill()
	_, _ = cmd1.Process.Wait()

	cmd2, err := startChromium(port2)
	if err != nil {
		t.Fatalf("start chromium 2: %v", err)
	}
	defer func() {
		_ = cmd2.Process.Kill()
		_, _ = cmd2.Process.Wait()
	}()

	// Expect manager to update to new port
	ok = waitForCondition(20*time.Second, func() bool {
		u := mgr.Current()
		return strings.Contains(u, fmt.Sprintf(":%d/", port2))
	})
	if !ok {
		t.Fatalf("did not update upstream to port %d; got: %q", port2, mgr.Current())
	}
}
