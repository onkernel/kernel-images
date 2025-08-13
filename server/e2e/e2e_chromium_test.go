package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	instanceoapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

const (
	headfulImage  = "onkernel/chromium-headful-test:latest"
	headlessImage = "onkernel/chromium-headless-test:latest"
	headfulName   = "chromium-headful-test"
	headlessName  = "chromium-headless-test"
	apiBaseURL    = "http://127.0.0.1:444"
	devtoolsHTTP  = "http://127.0.0.1:9222"
)

func TestChromiumHeadfulPersistence(t *testing.T) {
	runChromiumPersistenceFlow(t, headfulImage, headfulName, true)
}

func TestChromiumHeadlessPersistence(t *testing.T) {
	runChromiumPersistenceFlow(t, headlessImage, headlessName, false)
}

func runChromiumPersistenceFlow(t *testing.T, image, name string, runAsRoot bool) {
	t.Helper()

	// Ensure docker available early
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not available: %v", err)
	}

	// Step 1: start container
	mustStopContainer(name)
	env := map[string]string{
		"WITH_KERNEL_IMAGES_API": "true",
		"WITH_DOCKER":            "true",
		"CHROMIUM_FLAGS":         "--no-sandbox --disable-dev-shm-usage --disable-gpu --start-maximized --disable-software-rasterizer --remote-allow-origins=* --no-zygote --user-data-dir=/home/kernel/user-data",
	}
	if runAsRoot {
		env["RUN_AS_ROOT"] = "true"
	}
	if err := runContainer(image, name, env); err != nil {
		t.Fatalf("failed to start container %s: %v", image, err)
	}
	defer mustStopContainer(name)

	// Wait for API and devtools
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := waitHTTP(ctx, apiBaseURL+"/spec.yaml"); err != nil {
		t.Fatalf("api not ready: %v", err)
	}
	wsURL, err := waitDevtoolsWS(ctx)
	if err != nil {
		t.Fatalf("devtools not ready: %v", err)
	}

	// Step 2: set a cookie via CDP
	cookieName := "ki_e2e_cookie"
	cookieValue := fmt.Sprintf("v_%d", time.Now().UnixNano())
	if err := setCookieViaDevtools(ctx, wsURL, cookieName, cookieValue); err != nil {
		t.Fatalf("failed to set cookie: %v", err)
	}

	// Step 3: download user-data zip via API
	zipBytes, err := downloadUserDataZip(ctx)
	if err != nil {
		t.Fatalf("failed to download user data zip: %v", err)
	}
	// quick sanity check: is a valid zip
	if err := validateZip(zipBytes); err != nil {
		t.Fatalf("invalid zip downloaded: %v", err)
	}

	// Step 4: kill the container
	mustStopContainer(name)

	// Step 5: start a-new and wait
	if err := runContainer(image, name, env); err != nil {
		t.Fatalf("failed to restart container %s: %v", image, err)
	}
	if err := waitHTTP(ctx, apiBaseURL+"/spec.yaml"); err != nil {
		t.Fatalf("api not ready after restart: %v", err)
	}

	// Step 6: upload the zip back to /home/kernel/user-data
	if err := uploadUserDataZip(ctx, zipBytes); err != nil {
		t.Fatalf("failed to upload user data zip: %v", err)
	}

	// Step 7: restart chromium only via exec endpoint
	if err := restartChromiumViaAPI(ctx); err != nil {
		t.Fatalf("failed to restart chromium: %v", err)
	}

	// Wait for chromium up (supervisorctl avail as requested + devtools)
	if err := waitSupervisorAvail(ctx); err != nil {
		t.Fatalf("supervisorctl avail failed: %v", err)
	}
	wsURL, err = waitDevtoolsWS(ctx)
	if err != nil {
		t.Fatalf("devtools not ready after restart: %v", err)
	}

	// Final: verify cookie persists
	got, err := getCookieViaDevtools(ctx, wsURL, cookieName)
	if err != nil {
		t.Fatalf("failed to read cookie: %v", err)
	}
	if got != cookieValue {
		t.Fatalf("cookie mismatch after restore: got %q want %q", got, cookieValue)
	}
}

func runContainer(image, name string, env map[string]string) error {
	args := []string{
		"run", "-d",
		"--name", name,
		"--privileged",
		"--tmpfs", "/dev/shm:size=2g",
		"-p", "9222:9222",
		"-p", "444:10001",
	}
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, image)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func mustStopContainer(name string) {
	_ = exec.Command("docker", "kill", name).Run()
	_ = exec.Command("docker", "rm", "-f", name).Run()
}

func waitHTTP(ctx context.Context, url string) error {
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
	// The proxy ignores request path and forwards to the upstream ws path.
	return "ws://127.0.0.1:9222/devtools/browser", nil
}

func setCookieViaDevtools(ctx context.Context, wsURL, name, value string) error {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancel()
	cctx, cancel2 := chromedp.NewContext(allocCtx)
	defer cancel2()
	// We can set cookie for example.com to avoid site-dependence
	url := "https://example.com"
	return chromedp.Run(cctx,
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			params := []*network.CookieParam{
				{Name: name, Value: value, URL: url},
			}
			return network.SetCookies(params).Do(ctx)
		}),
	)
}

func getCookieViaDevtools(ctx context.Context, wsURL, name string) (string, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancel()
	cctx, cancel2 := chromedp.NewContext(allocCtx)
	defer cancel2()
	var cookies []*network.Cookie
	url := "https://example.com"
	err := chromedp.Run(cctx,
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			cs, err := network.GetCookies().WithURLs([]string{url}).Do(ctx)
			if err != nil {
				return err
			}
			cookies = cs
			return nil
		}),
	)
	if err != nil {
		return "", err
	}
	for _, c := range cookies {
		if c.Name == name {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("cookie %q not found", name)
}

func apiClient() (*instanceoapi.ClientWithResponses, error) {
	return instanceoapi.NewClientWithResponses(apiBaseURL, instanceoapi.WithHTTPClient(http.DefaultClient))
}

func downloadUserDataZip(ctx context.Context) ([]byte, error) {
	client, err := apiClient()
	if err != nil {
		return nil, err
	}
	params := &instanceoapi.DownloadDirZipParams{Path: "/home/kernel/user-data"}
	rsp, err := client.DownloadDirZip(ctx, params)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	return io.ReadAll(rsp.Body)
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

func restartChromiumViaAPI(ctx context.Context) error {
	client, err := apiClient()
	if err != nil {
		return err
	}
	// Restart chromium service
	req := instanceoapi.ProcessExecJSONRequestBody{
		Command: "supervisorctl",
		Args:    &[]string{"-c", "/etc/supervisor/supervisord.conf", "restart", "chromium"},
	}
	_, err = client.ProcessExecWithResponse(ctx, req)
	if err != nil {
		return err
	}
	return nil
}

func waitSupervisorAvail(ctx context.Context) error {
	client, err := apiClient()
	if err != nil {
		return err
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		req := instanceoapi.ProcessExecJSONRequestBody{
			Command: "supervisorctl",
			Args:    &[]string{"-c", "/etc/supervisor/supervisord.conf", "avail"},
		}
		rsp, err := client.ProcessExecWithResponse(ctx, req)
		if err == nil && rsp != nil && rsp.JSON200 != nil && rsp.JSON200.ExitCode != nil && *rsp.JSON200.ExitCode == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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
