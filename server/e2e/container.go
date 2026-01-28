package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	logctx "github.com/onkernel/kernel-images/server/lib/logger"
	instanceoapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// TestContainer manages a Docker container with dynamically allocated ports.
// This enables parallel test execution by giving each test its own ports.
type TestContainer struct {
	tb      testing.TB // supports both *testing.T and *testing.B
	Name    string
	Image   string
	APIPort int // dynamically allocated host port -> container 10001
	CDPPort int // dynamically allocated host port -> container 9222
	cmd     *exec.Cmd
	exitCh  <-chan error
	ctx     context.Context
}

// ContainerConfig holds optional configuration for container startup.
type ContainerConfig struct {
	Env        map[string]string
	HostAccess bool // Add host.docker.internal mapping
}

// NewTestContainer creates a new test container with dynamically allocated ports.
// Works with both *testing.T and *testing.B (any testing.TB).
func NewTestContainer(tb testing.TB, image string) *TestContainer {
	tb.Helper()

	apiPort, err := findFreePort()
	if err != nil {
		tb.Fatalf("failed to find free API port: %v", err)
	}

	cdpPort, err := findFreePort()
	if err != nil {
		tb.Fatalf("failed to find free CDP port: %v", err)
	}

	// Generate unique container name based on test name
	name := fmt.Sprintf("e2e-%s-%d", sanitizeTestName(tb.Name()), apiPort)

	return &TestContainer{
		tb:      tb,
		Name:    name,
		Image:   image,
		APIPort: apiPort,
		CDPPort: cdpPort,
	}
}

// findFreePort finds an available TCP port by binding to port 0.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// sanitizeTestName converts a test name to a valid container name suffix.
func sanitizeTestName(name string) string {
	// Replace slashes and other invalid characters
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	// Truncate to reasonable length
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

// Start starts the container with the given configuration.
func (c *TestContainer) Start(ctx context.Context, cfg ContainerConfig) error {
	c.ctx = ctx
	logger := logctx.FromContext(ctx)

	// Clean up any existing container with this name
	_ = c.cleanup(ctx)

	args := []string{
		"run",
		"--name", c.Name,
		"--privileged",
		"-p", fmt.Sprintf("%d:10001", c.APIPort),
		"-p", fmt.Sprintf("%d:9222", c.CDPPort),
		"--tmpfs", "/dev/shm:size=2g,mode=1777",
	}

	if cfg.HostAccess {
		args = append(args, "--add-host=host.docker.internal:host-gateway")
	}

	// Add environment variables
	// Ensure CHROMIUM_FLAGS includes --no-sandbox for CI
	envCopy := make(map[string]string)
	for k, v := range cfg.Env {
		envCopy[k] = v
	}
	if _, ok := envCopy["CHROMIUM_FLAGS"]; !ok {
		envCopy["CHROMIUM_FLAGS"] = "--no-sandbox"
	} else if !strings.Contains(envCopy["CHROMIUM_FLAGS"], "--no-sandbox") {
		envCopy["CHROMIUM_FLAGS"] = envCopy["CHROMIUM_FLAGS"] + " --no-sandbox"
	}

	for k, v := range envCopy {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, c.Image)

	logger.Info("[docker]", "action", "run", "container", c.Name, "apiPort", c.APIPort, "cdpPort", c.CDPPort)

	c.cmd = exec.CommandContext(ctx, "docker", args...)
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Create exit channel to detect container crashes
	exitCh := make(chan error, 1)
	go func() {
		exitCh <- c.cmd.Wait()
	}()
	c.exitCh = exitCh

	return nil
}

// Stop stops and removes the container.
func (c *TestContainer) Stop(ctx context.Context) error {
	return c.cleanup(ctx)
}

// cleanup removes the container if it exists.
func (c *TestContainer) cleanup(ctx context.Context) error {
	// Kill the container
	killCmd := exec.CommandContext(ctx, "docker", "kill", c.Name)
	_ = killCmd.Run() // Ignore errors - container may not exist

	// Remove the container
	rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", c.Name)
	return rmCmd.Run()
}

// APIBaseURL returns the URL for the container's API server.
func (c *TestContainer) APIBaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.APIPort)
}

// CDPURL returns the WebSocket URL for the container's DevTools proxy.
func (c *TestContainer) CDPURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d/", c.CDPPort)
}

// APIClient creates an OpenAPI client for this container's API.
func (c *TestContainer) APIClient() (*instanceoapi.ClientWithResponses, error) {
	return instanceoapi.NewClientWithResponses(c.APIBaseURL())
}

// WaitReady waits for the container's API to become ready.
func (c *TestContainer) WaitReady(ctx context.Context) error {
	return c.waitHTTPOrExit(ctx, c.APIBaseURL()+"/spec.yaml")
}

// waitHTTPOrExit waits for an HTTP endpoint to return 200 OK, or for the container to exit.
func (c *TestContainer) waitHTTPOrExit(ctx context.Context, url string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	client := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-c.exitCh:
			return fmt.Errorf("container exited while waiting for API: %w", err)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// ExitCh returns a channel that receives an error when the container exits.
func (c *TestContainer) ExitCh() <-chan error {
	return c.exitCh
}

// WaitDevTools waits for the CDP WebSocket endpoint to be ready.
func (c *TestContainer) WaitDevTools(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", c.CDPPort)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}
}

// APIClientNoKeepAlive creates an API client that doesn't reuse connections.
// This is useful after server restarts where existing connections may be stale.
func (c *TestContainer) APIClientNoKeepAlive() (*instanceoapi.ClientWithResponses, error) {
	transport := &http.Transport{
		DisableKeepAlives: true,
	}
	httpClient := &http.Client{Transport: transport}
	return instanceoapi.NewClientWithResponses(c.APIBaseURL(), instanceoapi.WithHTTPClient(httpClient))
}

// CDPAddr returns the TCP address for the container's DevTools proxy.
func (c *TestContainer) CDPAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.CDPPort)
}
