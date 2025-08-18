package e2e

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
	"text/template"

	_ "github.com/glebarez/sqlite"
	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	instanceoapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

const (
	defaultHeadfulImage  = "onkernel/chromium-headful-test:latest"
	defaultHeadlessImage = "onkernel/chromium-headless-test:latest"
	containerName        = "server-e2e-test"
	// With host networking, the API listens on 10001 directly on the host
	apiBaseURL = "http://127.0.0.1:10001"
)

var (
	headfulImage  = defaultHeadfulImage
	headlessImage = defaultHeadlessImage
)

func init() {
	// Prefer fully-specified images if provided
	if v := os.Getenv("E2E_CHROMIUM_HEADFUL_IMAGE"); v != "" {
		headfulImage = v
	}
	if v := os.Getenv("E2E_CHROMIUM_HEADLESS_IMAGE"); v != "" {
		headlessImage = v
	}
	// Otherwise, if a tag/sha is provided, use the CI-built images
	tag := os.Getenv("E2E_IMAGE_TAG")
	if tag == "" {
		tag = os.Getenv("E2E_IMAGE_SHA")
	}
	if tag != "" {
		if os.Getenv("E2E_CHROMIUM_HEADFUL_IMAGE") == "" {
			headfulImage = "onkernel/chromium-headful:" + tag
		}
		if os.Getenv("E2E_CHROMIUM_HEADLESS_IMAGE") == "" {
			headlessImage = "onkernel/chromium-headless:" + tag
		}
	}
}

// getPlaywrightPath returns the path to the playwright script
func getPlaywrightPath() string {
	return "./playwright"
}

// ensurePlaywrightDeps ensures playwright dependencies are installed
func ensurePlaywrightDeps(t *testing.T) {
	t.Helper()
	nodeModulesPath := getPlaywrightPath() + "/node_modules"
	if _, err := os.Stat(nodeModulesPath); os.IsNotExist(err) {
		t.Log("Installing playwright dependencies...")
		cmd := exec.Command("pnpm", "install")
		cmd.Dir = getPlaywrightPath()
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to install playwright dependencies: %v\nOutput: %s", err, string(output))
		}
		t.Log("Playwright dependencies installed successfully")
	}
}

func TestChromiumHeadfulUserDataSaving(t *testing.T) {
	ensurePlaywrightDeps(t)
	runChromiumUserDataSavingFlow(t, headfulImage, containerName, true)
}

func TestChromiumHeadlessPersistence(t *testing.T) {
	ensurePlaywrightDeps(t)
	runChromiumUserDataSavingFlow(t, headlessImage, containerName, true)
}

func runChromiumUserDataSavingFlow(t *testing.T, image, containerName string, runAsRoot bool) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				ts := a.Value.Time()
				return slog.String(slog.TimeKey, ts.UTC().Format(time.RFC3339))
			}
			return a
		},
	}))
	baseCtx := logctx.AddToContext(context.Background(), logger)
	logger.Info("[e2e]", "action", "starting chromium cookie saving flow", "image", image, "name", containerName, "runAsRoot", runAsRoot)
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("[precheck] docker not available: %v", err)
	}

	// Setup Phase
	layout := createTestTempLayout(t)
	logger.Info("[setup]", "base", layout.BaseDir, "zips", layout.ZipsDir, "restored", layout.RestoreDir)
	logger.Info("[setup]", "action", "ensuring container is not running", "container", containerName)
	if err := stopContainer(baseCtx, containerName); err != nil {
		t.Fatalf("[setup] failed to stop container %s: %v", containerName, err)
	}
	env := map[string]string{
		"WITH_KERNEL_IMAGES_API": "true",
		"WITH_DOCKER":            "true",
		"RUN_AS_ROOT":            fmt.Sprintf("%t", runAsRoot),
		"USER": func() string {
			if runAsRoot {
				return "root"
			}
			return "kernel"
		}(),
		"WIDTH":           "1024",
		"HEIGHT":          "768",
		"ENABLE_WEBRTC":   os.Getenv("ENABLE_WEBRTC"),
		"NEKO_ICESERVERS": os.Getenv("NEKO_ICESERVERS"),
	}
	if strings.Contains(image, "headful") {
		// headless image sets its own flags, so only do this for headful
		env["CHROMIUM_FLAGS"] = "--no-sandbox --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --no-zygote --password-store=basic --no-first-run"
	}
	logger.Info("[setup]", "action", "starting container", "image", image, "name", containerName)
	_, exitCh, err := runContainer(baseCtx, image, containerName, env)
	if err != nil {
		t.Fatalf("[setup] failed to start container %s: %v", image, err)
	}
	defer stopContainer(baseCtx, containerName)

	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()
	logger.Info("[setup]", "action", "waiting for API", "url", apiBaseURL+"/spec.yaml")
	if err := waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh); err != nil {
		_ = dumpContainerDiagnostics(ctx, containerName)
		t.Fatalf("[setup] api not ready: %v", err)
	}
	logger.Info("[setup]", "action", "waiting for DevTools WebSocket")
	wsURL, err := waitDevtoolsWS(ctx)
	if err != nil {
		t.Fatalf("[setup] devtools not ready: %v", err)
	}

	// Diagnostic Phase - Check file ownership and permissions before any navigations
	logger.Info("[diagnostic]", "action", "checking file ownership and permissions")
	if err := runCookieDebugScript(ctx, t); err != nil {
		logger.Warn("[diagnostic]", "action", "cookie debug script failed", "error", err)
	} else {
		logger.Info("[diagnostic]", "action", "cookie debug script completed successfully")
	}

	// Cookie Setting Phase
	cookieName := "e2e_cookie"
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	cookieValue := hex.EncodeToString(randBytes)
	serverURL, stopServer := startCookieTestServer(t, cookieName, cookieValue)
	defer stopServer()

	logger.Info("[cookies]", "action", "navigate set-cookie", "cookieName", cookieName, "cookieValue", cookieValue)
	if err := navigateAndEnsureCookie(ctx, wsURL, serverURL+"/set-cookie", cookieName, cookieValue, "initial"); err != nil {
		t.Fatalf("[cookies] failed to set/verify cookie: %v", err)
	}
	logger.Info("[cookies]", "action", "navigate get-cookies")
	if err := navigateAndEnsureCookie(ctx, wsURL, serverURL+"/get-cookies", cookieName, cookieValue, "initial-get-page"); err != nil {
		t.Fatalf("[cookies] failed to verify cookie on get-cookies: %v", err)
	}

	// Local Storage Setting Phase
	localStorageKey := "e2e_localstorage_key"
	randBytes = make([]byte, 16)
	rand.Read(randBytes)
	localStorageValue := hex.EncodeToString(randBytes)
	logger.Info("[localstorage]", "action", "set and verify localStorage")
	if err := setAndVerifyLocalStorage(ctx, wsURL, serverURL+"/set-cookie", localStorageKey, localStorageValue, "initial"); err != nil {
		t.Fatalf("[localstorage] failed to set/verify localStorage: %v", err)
	}

	// x.com Cookie Generation Phase
	logger.Info("[x-cookies]", "action", "navigate to x.com and verify guest_id cookie")
	if err := navigateToXAndVerifyCookie(ctx, wsURL, "initial"); err != nil {
		logger.Warn("[x-cookies]", "message", fmt.Sprintf("failed to navigate to x.com and verify cookie: %v", err))
	}

	// Restart & Persistence Testing Phase
	logger.Info("[restart]", "action", "stop chromium via supervisorctl")
	if err := stopChromiumViaSupervisord(ctx); err != nil {
		t.Fatalf("[restart] failed to stop chromium via supervisorctl: %v", err)
	}

	// Check file state after stopping
	logger.Info("[restart]", "action", "checking file state after stop")
	if err := runCookieDebugScript(ctx, t); err != nil {
		logger.Warn("[restart]", "action", "post-stop debug script failed", "error", err)
	} else {
		logger.Info("[restart]", "action", "post-stop debug script completed")
	}

	logger.Info("[snapshot]", "action", "download user-data zip")
	zipBytes, err := downloadUserDataZip(ctx)
	if err != nil {
		t.Fatalf("[snapshot] download zip: %v", err)
	}
	if err := validateZip(zipBytes); err != nil {
		t.Fatalf("[snapshot] invalid zip: %v", err)
	}
	zipPath := filepath.Join(layout.ZipsDir, "user-data-original.zip")
	if err := os.WriteFile(zipPath, zipBytes, 0600); err != nil {
		t.Fatalf("[snapshot] write zip: %v", err)
	}
	if err := unzipBytesToDir(zipBytes, layout.RestoreDir); err != nil {
		t.Fatalf("[snapshot] unzip: %v", err)
	}

	if err := verifyCookieInLocalSnapshot(ctx, layout.RestoreDir, cookieName, cookieValue); err != nil {
		logger.Warn("[snapshot]", "message", fmt.Sprintf("verify cookie in sqlite: %v", err))
	}
	if err := deleteLocalSingletonLockFiles(layout.RestoreDir); err != nil {
		t.Fatalf("[snapshot] delete local singleton locks: %v", err)
	}
	cleanZipBytes, err := zipDirToBytes(layout.RestoreDir)
	if err != nil {
		t.Fatalf("[snapshot] zip cleaned snapshot: %v", err)
	}
	cleanZipPath := filepath.Join(layout.ZipsDir, "user-data-cleaned.zip")
	if err := os.WriteFile(cleanZipPath, cleanZipBytes, 0600); err != nil {
		t.Fatalf("[snapshot] write cleaned zip: %v", err)
	}
	logger.Info("[snapshot]", "action", "delete remote user-data")
	if err := deleteDirectoryViaAPI(ctx, "/home/kernel/user-data"); err != nil {
		t.Fatalf("[snapshot] delete remote user-data: %v", err)
	}
	logger.Info("[snapshot]", "action", "upload cleaned zip", "bytes", len(cleanZipBytes))
	if err := uploadUserDataZip(ctx, cleanZipBytes); err != nil {
		t.Fatalf("[snapshot] upload cleaned zip: %v", err)
	}

	// Verify that the cookie exists in the container's cookies database after upload
	logger.Info("[snapshot]", "action", "verifying cookie in container database", "cookieName", cookieName)
	if err := verifyCookieInContainerDB(ctx, cookieName); err != nil {
		logger.Warn("[snapshot]", "message", fmt.Sprintf("cookie not found in container database: %v", err))
	}

	if err := startChromiumViaAPI(ctx); err != nil {
		t.Fatalf("[restart] start chromium: %v", err)
	}
	logger.Info("[restart]", "action", "wait for DevTools")
	wsURL, err = waitDevtoolsWS(ctx)
	if err != nil {
		t.Fatalf("[restart] devtools not ready: %v", err)
	}
	logger.Info("[restart]", "action", "sleep to init", "seconds", 5)
	time.Sleep(5 * time.Second)

	if err := navigateAndEnsureCookie(ctx, wsURL, serverURL+"/get-cookies", cookieName, cookieValue, "after-restart"); err != nil {
		t.Fatalf("[final] cookie not persisted after restart: %v", err)
	}
	logger.Info("[final]", "result", "cookie verified after restart")

	// Verify Local Storage persistence
	logger.Info("[final]", "action", "verifying localStorage persistence")
	if err := verifyLocalStorage(ctx, wsURL, serverURL+"/set-cookie", localStorageKey, localStorageValue, "after-restart"); err != nil {
		t.Fatalf("[final] localStorage not persisted after restart: %v", err)
	}
	logger.Info("[final]", "result", "localStorage verified after restart")

	logger.Info("[final]", "result", "all persistence mechanisms verified after restart")
}

func runContainer(ctx context.Context, image, name string, env map[string]string) (*exec.Cmd, <-chan error, error) {
	logger := logctx.FromContext(ctx)
	args := []string{
		"run",
		"--name", name,
		"--privileged",
		"--network=host",
		"--tmpfs", "/dev/shm:size=2g",
	}
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, image)

	logger.Info("[docker]", "action", "run", "args", strings.Join(args, " "))
	cmd := exec.Command("docker", args...)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
	}()

	return cmd, exitCh, nil
}

func stopContainer(ctx context.Context, name string) error {
	_ = exec.CommandContext(ctx, "docker", "kill", name).Run()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()

	// Wait loop to ensure the container is actually gone
	const maxWait = 10 * time.Second
	const pollInterval = 200 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	var lastCheckErr error
	for {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", name), "--format", "{{.Names}}")
		out, err := cmd.Output()
		if err != nil {
			// If docker itself fails, break out (maybe docker is gone)
			lastCheckErr = err
			break
		}
		names := strings.Fields(string(out))
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			break // container is gone
		}
		if time.Now().After(deadline) {
			lastCheckErr = fmt.Errorf("timeout waiting for container %s to be removed", name)
			break // give up after maxWait
		}
		time.Sleep(pollInterval)
	}

	if lastCheckErr != nil {
		return lastCheckErr
	}
	return nil
}

// dumpContainerDiagnostics prints container logs and inspect to structured logger for debugging startup failures
func dumpContainerDiagnostics(ctx context.Context, name string) error {
	logger := logctx.FromContext(ctx)
	logger.Info("[docker]", "action", "collecting logs", "name", name)
	logsCmd := exec.CommandContext(ctx, "docker", "logs", name)
	logsOut, _ := logsCmd.CombinedOutput()
	if len(logsOut) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(logsOut))
		for scanner.Scan() {
			logger.Info("[docker]", "action", "diag logs", "line", scanner.Text())
		}
	}
	logger.Info("[docker]", "action", "inspect", "name", name)
	inspectCmd := exec.CommandContext(ctx, "docker", "inspect", name)
	inspectOut, _ := inspectCmd.CombinedOutput()
	if len(inspectOut) > 0 {
		// Trim to a reasonable size
		const max = 64 * 1024
		if len(inspectOut) > max {
			inspectOut = inspectOut[:max]
		}
		scanner := bufio.NewScanner(bytes.NewReader(inspectOut))
		for scanner.Scan() {
			logger.Info("[docker]", "action", "diag inspect", "line", scanner.Text())
		}
	}
	return nil
}

func waitHTTPOrExit(ctx context.Context, url string, exitCh <-chan error) error {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 500 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil
		}
		if resp != nil && resp.Body != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exitCh:
			if err != nil {
				return fmt.Errorf("container exited while waiting for %s: %w", url, err)
			}
			return fmt.Errorf("container exited while waiting for %s", url)
		case <-ticker.C:
		}
	}
}

func waitTCP(ctx context.Context, hostport string) error {
	d := net.Dialer{Timeout: 2 * time.Second}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := d.DialContext(ctx, "tcp", hostport)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitDevtoolsWS(ctx context.Context) (string, error) {
	if err := waitTCP(ctx, "127.0.0.1:9222"); err != nil {
		return "", err
	}
	return "ws://127.0.0.1:9222/", nil
}

// startCookieTestServer starts an HTTP server listening on 0.0.0.0
// It serves two pages:
// - /set-cookie: sets a deterministic (passed in to server initialization) cookie if not present and displays cookie state
// - /get-cookies: just displays existing cookies without setting anything
func startCookieTestServer(t *testing.T, cookieName, cookieValue string) (url string, stop func()) {
	mux := http.NewServeMux()
	nameJS, err := json.Marshal(cookieName)
	if err != nil {
		t.Fatalf("failed to marshal cookieName: %v", err)
	}
	valueJS, err := json.Marshal(cookieValue)
	if err != nil {
		t.Fatalf("failed to marshal cookieValue: %v", err)
	}

	// Template for setting cookies
	const setCookieHTML = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Set Cookie Test</title></head>
<body>
<h1>Set Cookie Page</h1>
<script>
  (function(){
    var name = {{ .NameJS }};
    var targetValue = {{ .ValueJS }};
	console.log('document.cookie', document.cookie);
	console.log('name, value', name, targetValue);
    function getCookie(n){
      var m = document.cookie.split('; ').find(function(row){ return row.indexOf(n + '=') === 0; });
      return m ? m.substring(n.length + 1) : null;
    }
    if (document.cookie === '') {
      // Set cookie with 1 year expiration (31536000 seconds) to ensure it's persisted to disk
      document.cookie = name + '=' + targetValue + '; path=/; max-age=31536000';
      var status = document.createElement('p');
      status.innerText = 'Cookie was set';
      document.body.appendChild(status);
    } else {
      var status = document.createElement('p');
      status.innerText = 'Cookie already exists';
      document.body.appendChild(status);
    }
    var p = document.createElement('p');
    p.id = 'cookies';
    p.innerText = document.cookie;
    document.body.appendChild(p);
  })();
</script>
</body>
</html>`

	// Template for getting cookies only
	const getCookiesHTML = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Get Cookies Test</title></head>
<body>
<h1>Get Cookies Page</h1>
<p>This page only displays cookies, it does not set any.</p>
<p id="cookies"></p>
<script>
  document.getElementById('cookies').innerText = document.cookie || '(no cookies)';
</script>
</body>
</html>`

	setCookieTmpl := template.Must(template.New("set_cookie_page").Parse(setCookieHTML))
	var setCookieBuf bytes.Buffer
	if err := setCookieTmpl.Execute(&setCookieBuf, map[string]interface{}{
		"NameJS":  string(nameJS),
		"ValueJS": string(valueJS),
	}); err != nil {
		t.Fatalf("failed to execute set cookie page template: %v", err)
	}

	// Route that sets the cookie
	mux.HandleFunc("/set-cookie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, setCookieBuf.String())
	})

	// Route that only displays cookies
	mux.HandleFunc("/get-cookies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, getCookiesHTML)
	})

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("failed to start cookie test server: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	// figure out the random port assigned
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	url = "http://127.0.0.1:" + port
	stop = func() {
		_ = srv.Shutdown(context.Background())
	}
	return url, stop
}

// navigateAndEnsureCookie opens the given URL and asserts that the page's #cookies
// element contains name=value. It is idempotent and used before/after restarts.
func navigateAndEnsureCookie(ctx context.Context, wsURL, url, cookieName, cookieValue string, label string) error {
	logger := logctx.FromContext(ctx)

	// Run playwright script
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
		"navigate-and-ensure-cookie",
		"--url", url,
		"--cookie-name", cookieName,
		"--cookie-value", cookieValue,
		"--label", label,
		"--ws-url", wsURL,
		"--timeout", "45000",
	)
	cmd.Dir = getPlaywrightPath()

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Info("[playwright]", "action", "navigate-and-ensure-cookie failed", "output", string(output))
		return fmt.Errorf("playwright navigate-and-ensure-cookie failed: %w, output: %s", err, string(output))
	}

	logger.Info("[playwright]", "action", "navigate-and-ensure-cookie success", "output", string(output))
	return nil
}

// setAndVerifyLocalStorage sets a localStorage key-value pair and verifies it was set correctly
func setAndVerifyLocalStorage(ctx context.Context, wsURL, url, key, value, label string) error {
	logger := logctx.FromContext(ctx)

	// Run playwright script
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
		"set-localstorage",
		"--url", url,
		"--key", key,
		"--value", value,
		"--label", label,
		"--ws-url", wsURL,
		"--timeout", "45000",
	)
	cmd.Dir = getPlaywrightPath()

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Info("[playwright]", "action", "set-localstorage failed", "output", string(output))
		return fmt.Errorf("playwright set-localstorage failed: %w, output: %s", err, string(output))
	}

	logger.Info("[playwright]", "action", "set-localstorage success", "output", string(output))
	return nil
}

// verifyLocalStorage verifies that a localStorage key-value pair exists
func verifyLocalStorage(ctx context.Context, wsURL, url, key, value, label string) error {
	logger := logctx.FromContext(ctx)

	// Run playwright script
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
		"verify-localstorage",
		"--url", url,
		"--key", key,
		"--value", value,
		"--label", label,
		"--ws-url", wsURL,
		"--timeout", "45000",
	)
	cmd.Dir = getPlaywrightPath()

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Info("[playwright]", "action", "verify-localstorage failed", "output", string(output))
		return fmt.Errorf("playwright verify-localstorage failed: %w, output: %s", err, string(output))
	}

	logger.Info("[playwright]", "action", "verify-localstorage success", "output", string(output))
	return nil
}

// navigateToXAndVerifyCookie navigates to x.com and then to news.ycombinator.com to generate cookies,
// then verifies that the guest_id cookie was created for .x.com
func navigateToXAndVerifyCookie(ctx context.Context, wsURL string, label string) error {
	logger := logctx.FromContext(ctx)

	// Run playwright script to navigate to x.com and back
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
		"navigate-to-x-and-back",
		"--label", label,
		"--ws-url", wsURL,
		"--timeout", "45000",
	)
	cmd.Dir = getPlaywrightPath()

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Info("[playwright]", "action", "navigate-to-x-and-back failed", "output", string(output))
		return fmt.Errorf("playwright navigate-to-x-and-back failed: %w, output: %s", err, string(output))
	}

	logger.Info("[playwright]", "action", "navigate-to-x-and-back success", "output", string(output))

	// Now verify the cookie was created by querying the database
	logger.Info("[cookie-verify]", "action", "verifying guest_id cookie for .x.com")

	// wild: it takes about 10 seconds for cookies to flush to disk, and a supervisorctl stop / sigterm does not force it either. So we sleep
	time.Sleep(15 * time.Second)

	// Execute SQLite query to check for the cookie
	sqlQuery := `SELECT creation_utc,host_key,name,value,encrypted_value,last_update_utc FROM cookies WHERE host_key=".x.com" AND name="guest_id";`

	// Find the Cookies database file path
	cookiesDBPath := "/home/kernel/user-data/Default/Cookies"

	stdout, err := execCombinedOutput(ctx, "sqlite3", []string{cookiesDBPath, "-header", "-column", sqlQuery})
	if err != nil {
		return fmt.Errorf("failed to execute sqlite3 query on primary path: %w, output: %s", err, stdout)
	}

	// Log the raw output for debugging
	logger.Info("[cookie-verify]", "action", "sqlite3 output", "stdout", stdout)

	// Check if the output contains the expected cookie
	if !strings.Contains(stdout, ".x.com") || !strings.Contains(stdout, "guest_id") {
		logger.Error("[cookie-verify]", "action", "guest_id cookie not found", "output", stdout)
		return fmt.Errorf("guest_id cookie for .x.com not found in database output: %s", stdout)
	}

	logger.Info("[cookie-verify]", "action", "guest_id cookie verified successfully", "output", stdout)
	return nil
}

func apiClient() (*instanceoapi.ClientWithResponses, error) {
	return instanceoapi.NewClientWithResponses(apiBaseURL, instanceoapi.WithHTTPClient(http.DefaultClient))
}

// RemoteExecError represents a non-zero exit from a remote exec, exposing exit code and combined output
type RemoteExecError struct {
	Command  string
	Args     []string
	ExitCode int
	Output   string
}

func (e *RemoteExecError) Error() string {
	return fmt.Sprintf("remote exec %s exited with code %d", e.Command, e.ExitCode)
}

// execCombinedOutput runs a command via the remote API and returns combined stdout+stderr and an error if exit code != 0
func execCombinedOutput(ctx context.Context, command string, args []string) (string, error) {
	client, err := apiClient()
	if err != nil {
		return "", err
	}

	req := instanceoapi.ProcessExecJSONRequestBody{
		Command: command,
		Args:    &args,
	}

	rsp, err := client.ProcessExecWithResponse(ctx, req)
	if err != nil {
		return "", err
	}
	if rsp.JSON200 == nil {
		return "", fmt.Errorf("remote exec failed: %s body=%s", rsp.Status(), string(rsp.Body))
	}

	var stdout, stderr string
	if rsp.JSON200.StdoutB64 != nil && *rsp.JSON200.StdoutB64 != "" {
		if b, decErr := base64.StdEncoding.DecodeString(*rsp.JSON200.StdoutB64); decErr == nil {
			stdout = string(b)
		}
	}
	if rsp.JSON200.StderrB64 != nil && *rsp.JSON200.StderrB64 != "" {
		if b, decErr := base64.StdEncoding.DecodeString(*rsp.JSON200.StderrB64); decErr == nil {
			stderr = string(b)
		}
	}
	combined := stdout + stderr

	exitCode := 0
	if rsp.JSON200.ExitCode != nil {
		exitCode = *rsp.JSON200.ExitCode
	}
	if exitCode != 0 {
		return combined, &RemoteExecError{Command: command, Args: args, ExitCode: exitCode, Output: combined}
	}
	return combined, nil
}

func downloadUserDataZip(ctx context.Context) ([]byte, error) {
	client, err := apiClient()
	if err != nil {
		return nil, err
	}
	params := &instanceoapi.DownloadDirZipParams{Path: "/home/kernel/user-data"}
	rsp, err := client.DownloadDirZipWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	if rsp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("unexpected status downloading zip: %s body=%s", rsp.Status(), string(rsp.Body))
	}
	return rsp.Body, nil
}

func uploadUserDataZip(ctx context.Context, zipBytes []byte) error {
	client, err := apiClient()
	if err != nil {
		return err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("zip_file", "user-data.zip")
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, bytes.NewReader(zipBytes)); err != nil {
		return err
	}
	if err := w.WriteField("dest_path", "/home/kernel/user-data"); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_, err = client.UploadZipWithBodyWithResponse(ctx, w.FormDataContentType(), &body)
	return err
}

func startChromiumViaAPI(ctx context.Context) error {
	if out, err := execCombinedOutput(ctx, "supervisorctl", []string{"-c", "/etc/supervisor/supervisord.conf", "start", "chromium"}); err != nil {
		return fmt.Errorf("failed to start chromium: %w, output: %s", err, out)
	}
	// Ensure process fully running before proceeding
	if err := waitForProgramStates(ctx, "chromium", []string{"RUNNING"}, 10*time.Second); err != nil {
		return err
	}
	return nil
}

func deleteDirectoryViaAPI(ctx context.Context, path string) error {
	client, err := apiClient()
	if err != nil {
		return err
	}
	body := instanceoapi.DeleteDirectoryJSONRequestBody{Path: path}
	rsp, err := client.DeleteDirectoryWithResponse(ctx, body)
	if err != nil {
		return err
	}
	if rsp.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status deleting directory: %s body=%s", rsp.Status(), string(rsp.Body))
	}
	return nil
}

func validateZip(b []byte) error {
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	// Ensure at least one file
	if len(r.File) == 0 {
		return fmt.Errorf("empty zip")
	}
	// Try opening first file header to sanity-check
	f := r.File[0]
	rc, err := f.Open()
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()
	return nil
}

type testLayout struct {
	BaseDir    string
	ZipsDir    string
	RestoreDir string
}

// createTestTempLayout creates .tmp/userdata-test-{timestamp}/ with subdirs for zips and the restored userdata directory (i.e. after saving and preparing for reuse)
func createTestTempLayout(t *testing.T) testLayout {
	// Base under repo local .tmp
	base := filepath.Join(".tmp", fmt.Sprintf("userdata-test-%d", time.Now().UnixNano()))
	paths := []string{
		base,
		filepath.Join(base, "zips"),
		filepath.Join(base, "restored"),
	}
	for _, p := range paths {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatalf("create temp dir %s: %v", p, err)
		}
	}
	return testLayout{
		BaseDir:    base,
		ZipsDir:    filepath.Join(base, "zips"),
		RestoreDir: filepath.Join(base, "restored"),
	}
}

// unzipBytesToDir extracts a zip archive (in-memory) into destDir
func unzipBytesToDir(b []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range r.File {
		// Sanitize name
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") {
			return fmt.Errorf("invalid zip path: %s", f.Name)
		}
		abs := filepath.Join(destDir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(abs, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(w, rc); err != nil {
			w.Close()
			rc.Close()
			return err
		}
		w.Close()
		rc.Close()
	}
	return nil
}

// zipDirToBytes zips the contents of dir (no extra top-level folder) to bytes
func zipDirToBytes(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	defer zw.Close()

	// Walk dir
	root := filepath.Clean(dir)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			_, err := zw.Create(rel + "/")
			return err
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = rel
		fh.Method = zip.Deflate
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, f)
		f.Close()
		return copyErr
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// verifyCookieInLocalSnapshot verifies cookie presence in local unzipped snapshot
func verifyCookieInLocalSnapshot(ctx context.Context, root string, cookieName, wantValue string) error {
	logger := logctx.FromContext(ctx)
	candidates := []string{
		filepath.Join(root, "Default", "Network", "Cookies"),
		filepath.Join(root, "Default", "Cookies"),
	}

	logger.Info("[verify]", "action", "checking cookie database", "cookieName", cookieName, "wantValue", wantValue)

	for _, p := range candidates {
		logger.Info("[verify]", "action", "checking database", "path", p)
		ok, err := inspectLocalCookiesDB(ctx, p, cookieName, wantValue)
		if err == nil && ok {
			logger.Info("[verify]", "action", "cookie found", "path", p)
			return nil
		}
		if err != nil {
			logger.Warn("[verify]", "action", "database check failed", "path", p, "error", err)
		}
	}
	return fmt.Errorf("cookie %q not found in local snapshot", cookieName)
}

func inspectLocalCookiesDB(ctx context.Context, dbPath, cookieName, wantValue string) (bool, error) {
	logger := logctx.FromContext(ctx)

	// If db does not exist, skip
	if _, err := os.Stat(dbPath); err != nil {
		logger.Info("[inspect]", "action", "database file not found", "path", dbPath, "error", err)
		return false, nil
	}

	logger.Info("[inspect]", "action", "opening database", "path", dbPath)
	db, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(1)&_pragma=journal_mode(wal)")
	if err != nil {
		logger.Warn("[inspect]", "action", "failed to open database", "path", dbPath, "error", err)
		return false, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, value, length(encrypted_value) FROM cookies")
	if err != nil {
		logger.Warn("[inspect]", "action", "failed to query cookies", "path", dbPath, "error", err)
		return false, err
	}
	defer rows.Close()

	logger.Info("[inspect]", "action", "scanning cookies from database", "path", dbPath)
	cookieCount := 0
	for rows.Next() {
		var name, value string
		var encLen int64
		if err := rows.Scan(&name, &value, &encLen); err != nil {
			logger.Warn("[inspect]", "action", "failed to scan cookie row", "error", err)
			continue
		}
		cookieCount++
		logger.Info("[inspect]", "action", "found cookie", "name", name, "value", value, "encrypted", encLen > 0)

		if name == cookieName {
			if value == wantValue || encLen > 0 {
				logger.Info("[inspect]", "action", "target cookie found", "name", name, "value", value, "encrypted", encLen > 0)
				return true, nil
			}
		}
	}

	logger.Info("[inspect]", "action", "database scan complete", "path", dbPath, "totalCookies", cookieCount)
	return false, rows.Err()
}

// deleteLocalSingletonLockFiles removes Chromium singleton files in a local snapshot
func deleteLocalSingletonLockFiles(root string) error {
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket", "RunningChromeVersion"} {
		p := filepath.Join(root, name)
		_ = os.Remove(p)
	}
	return nil
}

// execCommandWithResponse is a helper function that executes a command via the remote API
// and handles the response parsing consistently across all callers
// Deprecated: use execCombinedOutput instead
func execCommandWithResponse(ctx context.Context, command string, args []string) (*instanceoapi.ProcessExecResponse, error) {
	client, err := apiClient()
	if err != nil {
		return nil, err
	}

	req := instanceoapi.ProcessExecJSONRequestBody{
		Command: command,
		Args:    &args,
	}

	return client.ProcessExecWithResponse(ctx, req)
}

// stopChromiumViaSupervisord stops chromium using supervisord via the remote API
func stopChromiumViaSupervisord(ctx context.Context) error {
	logger := logctx.FromContext(ctx)

	// Wait a bit for any pending I/O to complete
	logger.Info("[stop]", "action", "waiting for I/O flush", "seconds", 3)
	time.Sleep(3 * time.Second)

	// Now use supervisorctl to ensure it's fully stopped
	logger.Info("[stop]", "action", "stopping via supervisorctl")
	if out, stopErr := execCombinedOutput(ctx, "supervisorctl", []string{"-c", "/etc/supervisor/supervisord.conf", "stop", "chromium"}); stopErr != nil {
		return fmt.Errorf("failed to stop chromium via supervisorctl: %w, output: %s", stopErr, out)
	}

	// Accept either STOPPED or EXITED as terminal stopped states
	desiredStates := []string{"STOPPED", "EXITED"}
	if waitErr := waitForProgramStates(ctx, "chromium", desiredStates, 5*time.Second); waitErr != nil {
		return fmt.Errorf("chromium did not reach a stopped state: %w", waitErr)
	}

	// Allow a short grace period for I/O flush
	time.Sleep(1 * time.Second)
	return nil
}

// getProgramState returns the current supervisor state (e.g. RUNNING, STOPPED, EXITED) for the given program.
// It parses the output of `supervisorctl status` even if the command exits with a non-zero status code, which
// supervisorctl does when the target program is not in the RUNNING state.
func getProgramState(ctx context.Context, programName string) (string, error) {
	stdout, err := execCombinedOutput(ctx, "supervisorctl", []string{"-c", "/etc/supervisor/supervisord.conf", "status", programName})
	if err != nil {
		if execErr, ok := err.(*RemoteExecError); ok && execErr.ExitCode == 3 {
			stdout = execErr.Output
		} else {
			return "", err
		}
	}

	// Expected output example:
	// "chromium                        STOPPED   Sep 21 10:05 AM"
	// "chromium                        EXITED    Sep 21 10:05 AM (exit status 0)"
	fields := strings.Fields(stdout)
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected supervisorctl status output: %s", stdout)
	}
	return fields[1], nil
}

// waitForProgramStates polls supervisorctl status until the program reaches any of the desired states
// or the timeout expires.
func waitForProgramStates(ctx context.Context, programName string, desiredStates []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	contains := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}

	for {
		state, err := getProgramState(ctx, programName)
		if err == nil && contains(desiredStates, state) {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("timeout waiting for %s to reach states %v (last state %s)", programName, desiredStates, state)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// runCookieDebugScript executes the cookie debug script in the container to check file ownership and permissions
func runCookieDebugScript(ctx context.Context, t *testing.T) error {
	logger := logctx.FromContext(ctx)

	// Read the debug script content
	scriptContent, err := os.ReadFile("cookie_debug.sh")
	if err != nil {
		return fmt.Errorf("failed to read debug script: %w", err)
	}

	// Execute the script content directly via bash
	args := []string{"-c", string(scriptContent)}
	stdout, err := execCombinedOutput(ctx, "bash", args)
	if err != nil {
		return fmt.Errorf("failed to execute debug script: %w, output: %s", err, stdout)
	}

	logger.Info("[diagnostic]", "action", "debug script output")
	fmt.Fprint(t.Output(), stdout)
	return nil
}

// verifyCookieInContainerDB checks that the specified cookie exists in the cookies database on the container
func verifyCookieInContainerDB(ctx context.Context, cookieName string) error {
	logger := logctx.FromContext(ctx)

	// Execute SQLite query to check for the cookie
	sqlQuery := fmt.Sprintf(`SELECT creation_utc,host_key,name,value,encrypted_value,last_update_utc FROM cookies WHERE name="%s";`, cookieName)

	// Find the Cookies database file path
	cookiesDBPath := "/home/kernel/user-data/Default/Cookies"

	stdout, err := execCombinedOutput(ctx, "sqlite3", []string{cookiesDBPath, "-header", "-column", sqlQuery})
	if err != nil {
		return fmt.Errorf("failed to execute sqlite3 query: %w, output: %s", err, stdout)
	}

	// Log the raw output for debugging
	logger.Info("[container-cookie-verify]", "action", "sqlite3 output", "stdout", stdout)

	// Check if the output contains the expected cookie
	if !strings.Contains(stdout, cookieName) {
		logger.Error("[container-cookie-verify]", "action", "cookie not found", "cookieName", cookieName, "output", stdout)
		return fmt.Errorf("cookie %q not found in container database output: %s", cookieName, stdout)
	}

	logger.Info("[container-cookie-verify]", "action", "cookie verified successfully", "cookieName", cookieName, "output", stdout)
	return nil
}
