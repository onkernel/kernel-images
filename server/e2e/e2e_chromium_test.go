package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"log/slog"

	_ "github.com/glebarez/sqlite"
	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	instanceoapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err, "Failed to install playwright dependencies: %v\nOutput: %s", err, string(output))
		t.Log("Playwright dependencies installed successfully")
	}
}

func TestDisplayResolutionChange(t *testing.T) {
	image := headlessImage
	name := containerName + "-display"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Clean slate
	_ = stopContainer(baseCtx, name)

	// Start with default resolution
	env := map[string]string{
		"WIDTH":  "1024",
		"HEIGHT": "768",
	}

	// Start container
	_, exitCh, err := runContainer(baseCtx, image, name, env)
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()

	logger.Info("[setup]", "action", "waiting for API", "url", apiBaseURL+"/spec.yaml")
	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready: %v", err)

	client, err := apiClient()
	require.NoError(t, err, "failed to create API client: %v", err)

	// Get initial Xvfb resolution
	logger.Info("[test]", "action", "getting initial Xvfb resolution")
	initialWidth, initialHeight, err := getXvfbResolution(ctx)
	require.NoError(t, err, "failed to get initial Xvfb resolution: %v", err)
	logger.Info("[test]", "initial_resolution", fmt.Sprintf("%dx%d", initialWidth, initialHeight))
	require.Equal(t, 1024, initialWidth, "expected initial width 1024")
	require.Equal(t, 768, initialHeight, "expected initial height 768")

	// Test first resolution change: 1920x1080
	logger.Info("[test]", "action", "changing resolution to 1920x1080")
	width1 := 1920
	height1 := 1080
	req1 := instanceoapi.PatchDisplayJSONRequestBody{
		Width:  &width1,
		Height: &height1,
	}
	rsp1, err := client.PatchDisplayWithResponse(ctx, req1)
	require.NoError(t, err, "PATCH /display request failed: %v", err)
	require.Equal(t, http.StatusOK, rsp1.StatusCode(), "unexpected status: %s body=%s", rsp1.Status(), string(rsp1.Body))
	require.NotNil(t, rsp1.JSON200, "expected JSON200 response, got nil")
	require.NotNil(t, rsp1.JSON200.Width, "expected width in response")
	require.Equal(t, width1, *rsp1.JSON200.Width, "expected width %d in response", width1)
	require.NotNil(t, rsp1.JSON200.Height, "expected height in response")
	require.Equal(t, height1, *rsp1.JSON200.Height, "expected height %d in response", height1)

	// Wait a bit for Xvfb to fully restart
	logger.Info("[test]", "action", "waiting for Xvfb to stabilize")
	time.Sleep(3 * time.Second)

	// Verify new resolution via ps aux
	logger.Info("[test]", "action", "verifying new Xvfb resolution")
	newWidth1, newHeight1, err := getXvfbResolution(ctx)
	require.NoError(t, err, "failed to get new Xvfb resolution: %v", err)
	logger.Info("[test]", "new_resolution", fmt.Sprintf("%dx%d", newWidth1, newHeight1))
	require.Equal(t, width1, newWidth1, "expected Xvfb resolution %dx%d, got %dx%d", width1, height1, newWidth1, newHeight1)
	require.Equal(t, height1, newHeight1, "expected Xvfb resolution %dx%d, got %dx%d", width1, height1, newWidth1, newHeight1)

	// Test second resolution change: 1280x720
	logger.Info("[test]", "action", "changing resolution to 1280x720")
	width2 := 1280
	height2 := 720
	req2 := instanceoapi.PatchDisplayJSONRequestBody{
		Width:  &width2,
		Height: &height2,
	}
	rsp2, err := client.PatchDisplayWithResponse(ctx, req2)
	require.NoError(t, err, "PATCH /display request failed: %v", err)
	require.Equal(t, http.StatusOK, rsp2.StatusCode(), "unexpected status: %s body=%s", rsp2.Status(), string(rsp2.Body))
	require.NotNil(t, rsp2.JSON200, "expected JSON200 response, got nil")
	require.NotNil(t, rsp2.JSON200.Width, "expected width in response")
	require.Equal(t, width2, *rsp2.JSON200.Width, "expected width %d in response", width2)
	require.NotNil(t, rsp2.JSON200.Height, "expected height in response")
	require.Equal(t, height2, *rsp2.JSON200.Height, "expected height %d in response", height2)

	// Wait a bit for Xvfb to fully restart
	logger.Info("[test]", "action", "waiting for Xvfb to stabilize")
	time.Sleep(3 * time.Second)

	// Verify second resolution change via ps aux
	logger.Info("[test]", "action", "verifying second Xvfb resolution")
	newWidth2, newHeight2, err := getXvfbResolution(ctx)
	require.NoError(t, err, "failed to get second Xvfb resolution: %v", err)
	logger.Info("[test]", "final_resolution", fmt.Sprintf("%dx%d", newWidth2, newHeight2))
	require.Equal(t, width2, newWidth2, "expected Xvfb resolution %dx%d, got %dx%d", width2, height2, newWidth2, newHeight2)
	require.Equal(t, height2, newHeight2, "expected Xvfb resolution %dx%d, got %dx%d", width2, height2, newWidth2, newHeight2)

	logger.Info("[test]", "result", "all resolution changes verified successfully")
}

func TestExtensionUploadAndActivation(t *testing.T) {
	ensurePlaywrightDeps(t)
	image := headlessImage
	name := containerName + "-ext"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Clean slate
	_ = stopContainer(baseCtx, name)

	env := map[string]string{}
	// headless uses stealth defaults; no need to set CHROMIUM_FLAGS here

	// Start container
	_, exitCh, err := runContainer(baseCtx, image, name, env)
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready: %v", err)

	// Wait for DevTools
	_, err = waitDevtoolsWS(ctx)
	require.NoError(t, err, "devtools not ready: %v", err)

	// Build simple MV3 extension zip in-memory
	extDir := t.TempDir()
	manifest := `{
    "manifest_version": 3,
    "version": "1.0",
    "name": "My Test Extension",
    "description": "Test of a simple browser extension",
    "content_scripts": [
        {
            "matches": [
                "https://www.sfmoma.org/*"
            ],
            "js": [
                "content-script.js"
            ]
        }
    ]
}`
	contentScript := "document.title += \" -- Title updated by browser extension\";\n"
	err = os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(manifest), 0600)
	require.NoError(t, err, "write manifest: %v", err)
	err = os.WriteFile(filepath.Join(extDir, "content-script.js"), []byte(contentScript), 0600)
	require.NoError(t, err, "write content-script: %v", err)

	extZip, err := zipDirToBytes(extDir)
	require.NoError(t, err, "zip ext: %v", err)

	// Use new API to upload extension and restart Chromium
	{
		client, err := apiClient()
		require.NoError(t, err)
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		fw, err := w.CreateFormFile("extensions.zip_file", "ext.zip")
		require.NoError(t, err)
		_, err = io.Copy(fw, bytes.NewReader(extZip))
		require.NoError(t, err)
		err = w.WriteField("extensions.name", "testext")
		require.NoError(t, err)
		err = w.Close()
		require.NoError(t, err)
		start := time.Now()
		rsp, err := client.UploadExtensionsAndRestartWithBodyWithResponse(ctx, w.FormDataContentType(), &body)
		elapsed := time.Since(start)
		require.NoError(t, err, "uploadExtensionsAndRestart request error: %v", err)
		require.Equal(t, http.StatusCreated, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
		t.Logf("/chromium/upload-extensions-and-restart completed in %s (%d ms)", elapsed.String(), elapsed.Milliseconds())
	}

	// Verify the content script updated the title on an allowed URL
	{
		cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
			"verify-title-contains",
			"--url", "https://www.sfmoma.org/",
			"--substr", "Title updated by browser extension",
			"--ws-url", "ws://127.0.0.1:9222/",
			"--timeout", "45000",
		)
		cmd.Dir = getPlaywrightPath()
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "title verify failed: %v output=%s", err, string(out))
	}
}

func TestScreenshotHeadless(t *testing.T) {
	image := headlessImage
	name := containerName + "-screenshot-headless"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Clean slate
	_ = stopContainer(baseCtx, name)

	env := map[string]string{}

	// Start container
	_, exitCh, err := runContainer(baseCtx, image, name, env)
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready: %v", err)

	client, err := apiClient()
	require.NoError(t, err)

	// Whole-screen screenshot
	{
		rsp, err := client.TakeScreenshotWithResponse(ctx, instanceoapi.TakeScreenshotJSONRequestBody{})
		require.NoError(t, err, "screenshot request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for full screenshot: %s body=%s", rsp.Status(), string(rsp.Body))
		require.True(t, isPNG(rsp.Body), "response is not PNG (len=%d)", len(rsp.Body))
	}

	// Region screenshot (safe small region)
	{
		region := instanceoapi.ScreenshotRegion{X: 0, Y: 0, Width: 50, Height: 50}
		req := instanceoapi.TakeScreenshotJSONRequestBody{Region: &region}
		rsp, err := client.TakeScreenshotWithResponse(ctx, req)
		require.NoError(t, err, "region screenshot request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for region screenshot: %s body=%s", rsp.Status(), string(rsp.Body))
		require.True(t, isPNG(rsp.Body), "region response is not PNG (len=%d)", len(rsp.Body))
	}
}

func TestScreenshotHeadful(t *testing.T) {
	image := headfulImage
	name := containerName + "-screenshot-headful"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	// Clean slate
	_ = stopContainer(baseCtx, name)

	env := map[string]string{
		"WIDTH":  "1024",
		"HEIGHT": "768",
	}

	// Start container
	_, exitCh, err := runContainer(baseCtx, image, name, env)
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready: %v", err)

	client, err := apiClient()
	require.NoError(t, err)

	// Whole-screen screenshot
	{
		rsp, err := client.TakeScreenshotWithResponse(ctx, instanceoapi.TakeScreenshotJSONRequestBody{})
		require.NoError(t, err, "screenshot request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for full screenshot: %s body=%s", rsp.Status(), string(rsp.Body))
		require.True(t, isPNG(rsp.Body), "response is not PNG (len=%d)", len(rsp.Body))
	}

	// Region screenshot
	{
		region := instanceoapi.ScreenshotRegion{X: 0, Y: 0, Width: 80, Height: 60}
		req := instanceoapi.TakeScreenshotJSONRequestBody{Region: &region}
		rsp, err := client.TakeScreenshotWithResponse(ctx, req)
		require.NoError(t, err, "region screenshot request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for region screenshot: %s body=%s", rsp.Status(), string(rsp.Body))
		require.True(t, isPNG(rsp.Body), "region response is not PNG (len=%d)", len(rsp.Body))
	}
}

func TestInputEndpointsSmoke(t *testing.T) {
	image := headlessImage
	name := containerName + "-input-smoke"

	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	baseCtx := logctx.AddToContext(context.Background(), logger)

	if _, err := exec.LookPath("docker"); err != nil {
		require.NoError(t, err, "docker not available: %v", err)
	}

	_ = stopContainer(baseCtx, name)

	width, height := 1024, 768
	_, exitCh, err := runContainer(baseCtx, image, name, map[string]string{"WIDTH": strconv.Itoa(width), "HEIGHT": strconv.Itoa(height)})
	require.NoError(t, err, "failed to start container: %v", err)
	defer stopContainer(baseCtx, name)

	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Minute)
	defer cancel()

	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready: %v", err)

	client, err := apiClient()
	require.NoError(t, err)

	// press_key: tap Return
	{
		rsp, err := client.PressKeyWithResponse(ctx, instanceoapi.PressKeyJSONRequestBody{Keys: []string{"Return"}})
		require.NoError(t, err, "press_key request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for press_key: %s body=%s", rsp.Status(), string(rsp.Body))
	}

	// scroll: small vertical and horizontal ticks at center
	cx, cy := width/2, height/2
	{
		rsp, err := client.ScrollWithResponse(ctx, instanceoapi.ScrollJSONRequestBody{X: cx, Y: cy, DeltaX: lo.ToPtr(2), DeltaY: lo.ToPtr(-3)})
		require.NoError(t, err, "scroll request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for scroll: %s body=%s", rsp.Status(), string(rsp.Body))
	}
	// drag_mouse: simple short drag path
	{
		rsp, err := client.DragMouseWithResponse(ctx, instanceoapi.DragMouseJSONRequestBody{
			Path: [][]int{{cx - 10, cy - 10}, {cx + 10, cy + 10}},
		})
		require.NoError(t, err, "drag_mouse request error: %v", err)
		require.Equal(t, http.StatusOK, rsp.StatusCode(), "unexpected status for drag_mouse: %s body=%s", rsp.Status(), string(rsp.Body))
	}
}

// isPNG returns true if data starts with the PNG magic header
func isPNG(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i := 0; i < 8; i++ {
		if data[i] != sig[i] {
			return false
		}
	}
	return true
}

func runContainer(ctx context.Context, image, name string, env map[string]string) (*exec.Cmd, <-chan error, error) {
	logger := logctx.FromContext(ctx)
	args := []string{
		"run",
		"--name", name,
		"--privileged",
		"-p", "10001:10001", // API server
		"-p", "9222:9222", // DevTools proxy
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

// getXvfbResolution extracts the Xvfb resolution from the ps aux output
// It looks for the Xvfb command line which contains "-screen 0 WIDTHxHEIGHTx24"
func getXvfbResolution(ctx context.Context) (width, height int, err error) {
	logger := logctx.FromContext(ctx)

	// Get ps aux output
	stdout, err := execCombinedOutput(ctx, "ps", []string{"aux"})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to execute ps aux: %w, output: %s", err, stdout)
	}

	logger.Info("[xvfb-resolution]", "action", "parsing ps aux output")

	// Look for Xvfb line
	// Expected format: "root ... Xvfb :1 -screen 0 1920x1080x24 ..."
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "Xvfb") {
			continue
		}
		logger.Info("[xvfb-resolution]", "line", line)

		// Parse the screen parameter
		// Look for pattern: "-screen 0 WIDTHxHEIGHTx24"
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "-screen" && i+2 < len(fields) {
				// Next field should be "0", and the one after should be the resolution
				screenSpec := fields[i+2]
				logger.Info("[xvfb-resolution]", "screen_spec", screenSpec)

				// Parse WIDTHxHEIGHTx24
				parts := strings.Split(screenSpec, "x")
				if len(parts) >= 2 {
					w, err := strconv.Atoi(parts[0])
					if err != nil {
						return 0, 0, fmt.Errorf("failed to parse width from %q: %w", screenSpec, err)
					}
					h, err := strconv.Atoi(parts[1])
					if err != nil {
						return 0, 0, fmt.Errorf("failed to parse height from %q: %w", screenSpec, err)
					}
					logger.Info("[xvfb-resolution]", "parsed", fmt.Sprintf("%dx%d", w, h))
					return w, h, nil
				}
			}
		}
	}

	return 0, 0, fmt.Errorf("Xvfb process not found in ps aux output")
}
