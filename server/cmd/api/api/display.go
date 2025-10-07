package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// PatchDisplay updates the display configuration. When require_idle
// is true (default), it refuses to resize while live view or recording/replay is active.
// This method automatically detects whether the system is running with Xorg (headful)
// or Xvfb (headless) and uses the appropriate method to change resolution.
func (s *ApiService) PatchDisplay(ctx context.Context, req oapi.PatchDisplayRequestObject) (oapi.PatchDisplayResponseObject, error) {
	log := logger.FromContext(ctx)
	if req.Body == nil {
		return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "missing request body"}}, nil
	}

	// Check if resolution change is requested
	if req.Body.Width == nil && req.Body.Height == nil {
		return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no display parameters to update"}}, nil
	}

	// Get current resolution with refresh rate
	currentWidth, currentHeight, currentRefreshRate := s.getCurrentResolution(ctx)
	width := currentWidth
	height := currentHeight
	refreshRate := currentRefreshRate

	if req.Body.Width != nil {
		width = *req.Body.Width
	}
	if req.Body.Height != nil {
		height = *req.Body.Height
	}
	if req.Body.RefreshRate != nil {
		refreshRate = int(*req.Body.RefreshRate)
	}

	if width <= 0 || height <= 0 {
		return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid width/height"}}, nil
	}

	log.Info("resolution change requested", "width", width, "height", height, "refresh_rate", refreshRate)

	// Parse requireIdle flag (default true)
	requireIdle := true
	if req.Body.RequireIdle != nil {
		requireIdle = *req.Body.RequireIdle
	}

	// Check if resize is safe (no active sessions or recordings)
	if requireIdle {
		live := s.getActiveNekoSessions(ctx)
		isRecording := s.anyRecordingActive(ctx)
		isReplaying := false // replay not currently implemented
		resizableNow := (live == 0) && !isRecording && !isReplaying

		log.Info("checking if resize is safe", "live_sessions", live, "is_recording", isRecording, "is_replaying", isReplaying, "resizable", resizableNow)

		if !resizableNow {
			return oapi.PatchDisplay409JSONResponse{
				ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{
					Message: "resize refused: live view or recording/replay active",
				},
			}, nil
		}
	}

	// Detect display mode (xorg or xvfb)
	displayMode := s.detectDisplayMode(ctx)

	// Parse restartChromium flag (default depends on mode)
	restartChrome := (displayMode == "xvfb") // default true for xvfb, false for xorg
	if req.Body.RestartChromium != nil {
		restartChrome = *req.Body.RestartChromium
	}

	// Route to appropriate resolution change handler
	var err error
	if displayMode == "xorg" {
		if s.isNekoEnabled() {
			log.Info("using Neko API for Xorg resolution change")
			err = s.setResolutionXorgViaNeko(ctx, width, height, refreshRate, restartChrome)
		} else {
			log.Info("using xrandr for Xorg resolution change (Neko disabled)")
			err = s.setResolutionXorgViaXrandr(ctx, width, height, refreshRate, restartChrome)
		}
	} else {
		log.Info("using Xvfb restart for resolution change")
		err = s.setResolutionXvfb(ctx, width, height, restartChrome)
	}

	if err != nil {
		log.Error("failed to change resolution", "error", err)
		return oapi.PatchDisplay500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to change resolution: %s", err.Error()),
			},
		}, nil
	}

	// Return success with the new dimensions
	return oapi.PatchDisplay200JSONResponse{
		Width:  &width,
		Height: &height,
	}, nil
}

// detectDisplayMode detects whether we're running Xorg (headful) or Xvfb (headless)
func (s *ApiService) detectDisplayMode(ctx context.Context) string {
	log := logger.FromContext(ctx)
	checkCmd := []string{"-lc", "supervisorctl status xvfb >/dev/null 2>&1 && echo 'xvfb' || echo 'xorg'"}
	checkReq := oapi.ProcessExecRequest{Command: "bash", Args: &checkCmd}
	checkResp, _ := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &checkReq})

	if execResp, ok := checkResp.(oapi.ProcessExec200JSONResponse); ok {
		if execResp.StdoutB64 != nil {
			if output, err := base64.StdEncoding.DecodeString(*execResp.StdoutB64); err == nil {
				outputStr := strings.TrimSpace(string(output))
				if outputStr == "xvfb" {
					log.Info("detected Xvfb display (headless mode)")
					return "xvfb"
				}
			}
		}
	}
	log.Info("detected Xorg display (headful mode)")
	return "xorg"
}

// setResolutionXorgViaNeko changes resolution for Xorg using Neko API
func (s *ApiService) setResolutionXorgViaNeko(ctx context.Context, width, height, refreshRate int, restartChrome bool) error {
	log := logger.FromContext(ctx)

	if err := s.setResolutionViaNeko(ctx, width, height, refreshRate); err != nil {
		return fmt.Errorf("failed to change resolution via Neko API: %w", err)
	}

	if restartChrome {
		log.Info("restarting chromium after resolution change")
		restartCmd := []string{"-lc", "supervisorctl restart chromium"}
		restartReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartCmd}
		if restartResp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartReq}); err != nil {
			log.Error("failed to restart chromium", "error", err)
		} else if execResp, ok := restartResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
			}
		}
	}

	return nil
}

// setResolutionXorgViaXrandr changes resolution for Xorg using xrandr (fallback when Neko is disabled)
func (s *ApiService) setResolutionXorgViaXrandr(ctx context.Context, width, height, refreshRate int, restartChrome bool) error {
	log := logger.FromContext(ctx)
	display := s.resolveDisplayFromEnv()

	// Build xrandr command - if refresh rate is specified, use the specific modeline
	var xrandrCmd string
	if refreshRate > 0 {
		modeName := fmt.Sprintf("%dx%d_%d.00", width, height, refreshRate)
		xrandrCmd = fmt.Sprintf("xrandr --output default --mode %s", modeName)
		log.Info("using specific modeline", "mode", modeName)
	} else {
		xrandrCmd = fmt.Sprintf("xrandr -s %dx%d", width, height)
	}

	args := []string{"-lc", xrandrCmd}
	env := map[string]string{"DISPLAY": display}
	execReq := oapi.ProcessExecRequest{Command: "bash", Args: &args, Env: &env}
	resp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &execReq})
	if err != nil {
		return fmt.Errorf("failed to execute xrandr: %w", err)
	}

	switch r := resp.(type) {
	case oapi.ProcessExec200JSONResponse:
		if r.ExitCode != nil && *r.ExitCode != 0 {
			var stderr string
			if r.StderrB64 != nil {
				if b, decErr := base64.StdEncoding.DecodeString(*r.StderrB64); decErr == nil {
					stderr = strings.TrimSpace(string(b))
				}
			}
			if stderr == "" {
				stderr = "xrandr returned non-zero exit code"
			}
			return fmt.Errorf("xrandr failed: %s", stderr)
		}
		log.Info("resolution updated via xrandr", "display", display, "width", width, "height", height)

		if restartChrome {
			log.Info("restarting chromium after resolution change")
			restartCmd := []string{"-lc", "supervisorctl restart chromium"}
			restartReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartCmd}
			if restartResp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartReq}); err != nil {
				log.Error("failed to restart chromium", "error", err)
			} else if execResp, ok := restartResp.(oapi.ProcessExec200JSONResponse); ok {
				if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
					log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
				}
			}
		}
		return nil
	case oapi.ProcessExec400JSONResponse:
		return fmt.Errorf("bad request: %s", r.Message)
	case oapi.ProcessExec500JSONResponse:
		return fmt.Errorf("internal error: %s", r.Message)
	default:
		return fmt.Errorf("unexpected response from process exec")
	}
}

// setResolutionXvfb changes resolution for Xvfb by updating config and restarting services
func (s *ApiService) setResolutionXvfb(ctx context.Context, width, height int, restartChrome bool) error {
	log := logger.FromContext(ctx)
	log.Info("updating Xvfb resolution requires restart", "width", width, "height", height)

	// Update supervisor config to include environment variables
	log.Info("updating xvfb supervisor config with new dimensions")
	removeEnvCmd := []string{"-lc", `sed -i '/^environment=/d' /etc/supervisor/conf.d/services/xvfb.conf`}
	removeEnvReq := oapi.ProcessExecRequest{Command: "bash", Args: &removeEnvCmd}
	s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &removeEnvReq})

	// Add the environment line with WIDTH and HEIGHT
	addEnvCmd := []string{"-lc", fmt.Sprintf(`sed -i '/\[program:xvfb\]/a environment=WIDTH="%d",HEIGHT="%d",DPI="96",DISPLAY=":1"' /etc/supervisor/conf.d/services/xvfb.conf`, width, height)}
	addEnvReq := oapi.ProcessExecRequest{Command: "bash", Args: &addEnvCmd}
	configResp, configErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &addEnvReq})
	if configErr != nil {
		return fmt.Errorf("failed to update xvfb config: %w", configErr)
	}

	// Check if config update succeeded
	if execResp, ok := configResp.(oapi.ProcessExec200JSONResponse); ok {
		if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
			log.Error("failed to update xvfb config", "exit_code", *execResp.ExitCode)
			return fmt.Errorf("failed to update xvfb config")
		}
	}

	// Reload supervisor configuration
	log.Info("reloading supervisor configuration")
	reloadCmd := []string{"-lc", "supervisorctl reread && supervisorctl update"}
	reloadReq := oapi.ProcessExecRequest{Command: "bash", Args: &reloadCmd}
	if _, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &reloadReq}); err != nil {
		log.Error("failed to reload supervisor config", "error", err)
	}

	// Restart xvfb with new configuration
	log.Info("restarting xvfb with new resolution")
	restartXvfbCmd := []string{"-lc", "supervisorctl restart xvfb"}
	restartXvfbReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartXvfbCmd}
	xvfbResp, xvfbErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartXvfbReq})
	if xvfbErr != nil {
		return fmt.Errorf("failed to restart Xvfb: %w", xvfbErr)
	}

	// Check if Xvfb restart succeeded
	if execResp, ok := xvfbResp.(oapi.ProcessExec200JSONResponse); ok {
		if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
			return fmt.Errorf("Xvfb restart failed")
		}
	}

	// Wait for Xvfb to be ready
	log.Info("waiting for Xvfb to be ready")
	waitCmd := []string{"-lc", "sleep 2"}
	waitReq := oapi.ProcessExecRequest{Command: "bash", Args: &waitCmd}
	s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &waitReq})

	if restartChrome {
		log.Info("restarting chromium after Xvfb restart")
		restartChromeCmd := []string{"-lc", "supervisorctl restart chromium"}
		restartChromeReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartChromeCmd}
		if chromeResp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartChromeReq}); err != nil {
			log.Error("failed to restart chromium", "error", err)
		} else if execResp, ok := chromeResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
			}
		}
	}

	log.Info("Xvfb resolution updated", "width", width, "height", height)
	return nil
}

// anyRecordingActive returns true if any registered recorder is currently recording.
func (s *ApiService) anyRecordingActive(ctx context.Context) bool {
	for _, r := range s.recordManager.ListActiveRecorders(ctx) {
		if r.IsRecording(ctx) {
			return true
		}
	}
	return false
}

// getActiveNekoSessions queries the Neko API for active viewer sessions.
func (s *ApiService) getActiveNekoSessions(ctx context.Context) int {
	log := logger.FromContext(ctx)

	// Get authentication token
	token, err := s.getNekoToken(ctx)
	if err != nil {
		log.Debug("failed to get Neko token", "error", err)
		return 0
	}

	// Create HTTP client with short timeout
	client := &http.Client{
		Timeout: 500 * time.Millisecond,
	}

	// Query Neko sessions API
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:8080/api/sessions", nil)
	if err != nil {
		log.Debug("failed to create Neko API request", "error", err)
		return 0
	}

	// Add Bearer token authentication
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := client.Do(req)
	if err != nil {
		log.Debug("failed to query Neko API", "error", err)
		return 0
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode == http.StatusUnauthorized {
		log.Warn("Neko API returned 401, clearing cached token")
		s.clearNekoToken()
		return 0
	}
	if resp.StatusCode != http.StatusOK {
		log.Warn("Neko API returned non-OK status", "status", resp.StatusCode)
		return 0
	}

	// Parse response
	var sessions []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		log.Error("failed to parse Neko API response", "error", err)
		return 0
	}

	// Debug: log each session to understand what we're counting
	live := 0
	for i, session := range sessions {
		log.Info("neko session details", "index", i, "session", session)
		if stRaw, ok := session["state"]; ok {
			if st, ok := stRaw.(map[string]interface{}); ok {
				connected, _ := st["is_connected"].(bool)
				watching, _ := st["is_watching"].(bool)
				if connected && watching {
					live++
				}
			}
		}
	}

	log.Info("successfully queried Neko API", "active_sessions", live)
	return live
}

// resolveDisplayFromEnv returns the X display string, defaulting to ":1".
func (s *ApiService) resolveDisplayFromEnv() string {
	// Prefer KERNEL_IMAGES_API_DISPLAY_NUM, fallback to DISPLAY_NUM, default 1
	if v := strings.TrimSpace(os.Getenv("KERNEL_IMAGES_API_DISPLAY_NUM")); v != "" {
		return ":" + v
	}
	if v := strings.TrimSpace(os.Getenv("DISPLAY_NUM")); v != "" {
		return ":" + v
	}
	return ":1"
}

// getCurrentResolution returns the current display resolution and refresh rate by querying xrandr
func (s *ApiService) getCurrentResolution(ctx context.Context) (int, int, int) {
	log := logger.FromContext(ctx)
	display := s.resolveDisplayFromEnv()

	// Use xrandr to get current resolution
	cmd := exec.CommandContext(ctx, "bash", "-lc", "xrandr | grep -E '\\*' | awk '{print $1}'")
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", display))

	out, err := cmd.Output()
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		// Return default resolution on error
		return 1024, 768, 60
	}

	resStr := strings.TrimSpace(string(out))
	parts := strings.Split(resStr, "x")
	if len(parts) != 2 {
		log.Error("unexpected xrandr output format", "output", resStr)
		return 1024, 768, 60
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Error("failed to parse width", "error", err, "value", parts[0])
		return 1024, 768, 60
	}

	// Parse height and refresh rate (e.g., "1080_60.00" -> height=1080, rate=60)
	heightStr := parts[1]
	refreshRate := 60 // default
	if idx := strings.Index(heightStr, "_"); idx != -1 {
		rateStr := heightStr[idx+1:]
		heightStr = heightStr[:idx]
		// Parse the refresh rate (e.g., "60.00" -> 60)
		if rateFloat, err := strconv.ParseFloat(rateStr, 64); err == nil {
			refreshRate = int(rateFloat)
		}
	}

	height, err := strconv.Atoi(heightStr)
	if err != nil {
		log.Error("failed to parse height", "error", err, "value", heightStr)
		return 1024, 768, 60
	}

	return width, height, refreshRate
}

// isNekoEnabled checks if Neko service is enabled
func (s *ApiService) isNekoEnabled() bool {
	return os.Getenv("ENABLE_WEBRTC") == "true"
}

// getNekoToken obtains a bearer token from Neko API for authentication.
// It caches the token and reuses it for subsequent requests.
func (s *ApiService) getNekoToken(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)

	// Check if we have a cached token
	s.nekoTokenMu.RLock()
	cachedToken := s.nekoToken
	s.nekoTokenMu.RUnlock()

	if cachedToken != "" {
		return cachedToken, nil
	}

	// Need to obtain a new token
	s.nekoTokenMu.Lock()
	defer s.nekoTokenMu.Unlock()

	// Double-check in case another goroutine just obtained the token
	if s.nekoToken != "" {
		return s.nekoToken, nil
	}

	// Get admin credentials
	adminPassword := os.Getenv("NEKO_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin" // Default from neko.yaml
	}

	// Prepare login request
	loginReq := map[string]string{
		"username": "admin",
		"password": adminPassword,
	}

	loginBody, err := json.Marshal(loginReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %w", err)
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Call login endpoint
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://localhost:8080/api/login",
		strings.NewReader(string(loginBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call login API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response to get token
	var loginResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	log.Debug("neko login response", "response", loginResp)

	token, ok := loginResp["token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("login response did not contain a token")
	}

	// Cache the token
	s.nekoToken = token
	log.Info("successfully obtained Neko authentication token")

	return s.nekoToken, nil
}

// clearNekoToken clears the cached token, forcing a new login on next request
func (s *ApiService) clearNekoToken() {
	s.nekoTokenMu.Lock()
	defer s.nekoTokenMu.Unlock()
	s.nekoToken = ""
}

// setResolutionViaNeko delegates resolution change to Neko API
func (s *ApiService) setResolutionViaNeko(ctx context.Context, width, height, refreshRate int) error {
	log := logger.FromContext(ctx)

	// Get authentication token
	token, err := s.getNekoToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Neko token: %w", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Use default refresh rate if not specified
	if refreshRate <= 0 {
		refreshRate = 60
	}

	// Prepare request body for Neko's screen API
	screenConfig := map[string]interface{}{
		"width":  width,
		"height": height,
		"rate":   refreshRate,
	}

	body, err := json.Marshal(screenConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://localhost:8080/api/room/screen",
		strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Add Bearer token authentication
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Neko API: %w", err)
	}
	defer resp.Body.Close()

	// Handle 401 by clearing token and retrying once
	if resp.StatusCode == http.StatusUnauthorized {
		log.Warn("Neko API returned 401, clearing cached token and retrying")
		s.clearNekoToken()

		// Get fresh token
		token, err = s.getNekoToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get fresh Neko token: %w", err)
		}

		// Retry the request with fresh token
		req, err = http.NewRequestWithContext(ctx, "POST",
			"http://localhost:8080/api/room/screen",
			strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("failed to create retry request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to retry Neko API call: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Neko API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Info("successfully changed resolution via Neko API", "width", width, "height", height, "refresh_rate", refreshRate)
	return nil
}
