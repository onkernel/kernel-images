package e2e

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	"github.com/stretchr/testify/require"
)

// TestMV3ServiceWorkerRegistration tests that MV3 extensions with service workers
// are properly loaded and their service workers are active and responsive.
//
// This test verifies:
// 1. Extension can be uploaded and Chromium restarts successfully
// 2. Extension appears in chrome://extensions with an active service worker
// 3. Service worker responds to messages from the popup
func TestMV3ServiceWorkerRegistration(t *testing.T) {
	ensurePlaywrightDeps(t)

	image := headlessImage
	name := containerName + "-mv3-sw"

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

	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()

	logger.Info("[setup]", "action", "waiting for API", "url", apiBaseURL+"/spec.yaml")
	require.NoError(t, waitHTTPOrExit(ctx, apiBaseURL+"/spec.yaml", exitCh), "api not ready")

	// Wait for DevTools to be ready
	_, err = waitDevtoolsWS(ctx)
	require.NoError(t, err, "devtools not ready")

	// Upload the MV3 test extension
	logger.Info("[test]", "action", "uploading MV3 service worker test extension")
	uploadMV3TestExtension(t, ctx, logger)

	// Run playwright script to verify service worker
	logger.Info("[test]", "action", "verifying MV3 service worker via playwright")
	verifyMV3ServiceWorker(t, ctx, logger)

	logger.Info("[test]", "result", "MV3 service worker test passed")
}

// uploadMV3TestExtension uploads the test extension from test-extension directory.
func uploadMV3TestExtension(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	client, err := apiClient()
	require.NoError(t, err, "failed to create API client")

	// Get the path to the test extension
	// The test extension is in server/e2e/test-extension
	extDir, err := filepath.Abs("test-extension")
	require.NoError(t, err, "failed to get absolute path to test-extension")

	// Create zip of the extension
	extZip, err := zipDirToBytes(extDir)
	require.NoError(t, err, "failed to zip test extension")

	// Upload extension
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("extensions.zip_file", "mv3-test-ext.zip")
	require.NoError(t, err)
	_, err = io.Copy(fw, bytes.NewReader(extZip))
	require.NoError(t, err)
	err = w.WriteField("extensions.name", "mv3-service-worker-test")
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)

	start := time.Now()
	rsp, err := client.UploadExtensionsAndRestartWithBodyWithResponse(ctx, w.FormDataContentType(), &body)
	elapsed := time.Since(start)
	require.NoError(t, err, "uploadExtensionsAndRestart request error")
	require.Equal(t, http.StatusCreated, rsp.StatusCode(), "unexpected status: %s body=%s", rsp.Status(), string(rsp.Body))
	logger.Info("[extension]", "action", "uploaded", "elapsed", elapsed.String())
}

// verifyMV3ServiceWorker runs the playwright script to verify the service worker.
func verifyMV3ServiceWorker(t *testing.T, ctx context.Context, logger *slog.Logger) {
	t.Helper()

	cmd := exec.CommandContext(ctx, "pnpm", "exec", "tsx", "index.ts",
		"verify-mv3-service-worker",
		"--ws-url", "ws://127.0.0.1:9222/",
		"--timeout", "60000",
	)
	cmd.Dir = getPlaywrightPath()
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("[playwright]", "output", string(out))
	}
	require.NoError(t, err, "MV3 service worker verification failed: %v\noutput=%s", err, string(out))
	logger.Info("[playwright]", "output", string(out))
}
