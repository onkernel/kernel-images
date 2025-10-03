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

	// Get current resolution if only one dimension is provided
	currentWidth, currentHeight := s.getCurrentResolution(ctx)
	width := currentWidth
	height := currentHeight

	if req.Body.Width != nil {
		width = *req.Body.Width
	}
	if req.Body.Height != nil {
		height = *req.Body.Height
	}

	if width <= 0 || height <= 0 {
		return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid width/height"}}, nil
	}
	requireIdle := true
	if req.Body.RequireIdle != nil {
		requireIdle = *req.Body.RequireIdle
	}

	// Check current status if required
	if requireIdle {
		live := s.getActiveNekoSessions(ctx)
		isRecording := s.anyRecordingActive(ctx)
		isReplaying := false // replay not currently implemented
		resizableNow := (live == 0) && !isRecording && !isReplaying

		if !resizableNow {
			return oapi.PatchDisplay409JSONResponse{
				ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{
					Message: "resize refused: live view or recording/replay active",
				},
			}, nil
		}
	}

	// When Neko is enabled, delegate resolution changes to its API
	// Neko handles all the complexity of restarting X.org/Xvfb and Chromium
	if s.isNekoEnabled() {
		log.Info("delegating resolution change to Neko API", "width", width, "height", height)

		if err := s.setResolutionViaNeko(ctx, width, height); err != nil {
			log.Error("failed to change resolution via Neko API, falling back to direct method", "error", err)
			// Fall through to direct implementation
		} else {
			// Successfully changed via Neko
			return oapi.PatchDisplay200JSONResponse{
				Width:  &width,
				Height: &height,
			}, nil
		}
	}

	display := s.resolveDisplayFromEnv()

	// Detect if we're using Xorg (headful) or Xvfb (headless) by checking supervisor services
	// This is more reliable than checking xrandr support since xrandr might be installed
	// but not functional with Xvfb
	checkCmd := []string{"-lc", "supervisorctl status xvfb >/dev/null 2>&1 && echo 'xvfb' || echo 'xorg'"}
	checkReq := oapi.ProcessExecRequest{Command: "bash", Args: &checkCmd}
	checkResp, _ := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &checkReq})

	isXorg := true
	if execResp, ok := checkResp.(oapi.ProcessExec200JSONResponse); ok {
		if execResp.StdoutB64 != nil {
			if output, err := base64.StdEncoding.DecodeString(*execResp.StdoutB64); err == nil {
				outputStr := strings.TrimSpace(string(output))
				if outputStr == "xvfb" {
					isXorg = false
					log.Info("detected Xvfb display (headless mode)")
				} else {
					log.Info("detected Xorg display (headful mode)")
				}
			}
		}
	}

	if isXorg {
		// Xorg path: use xrandr
		args := []string{"-lc", fmt.Sprintf("xrandr -s %dx%d", width, height)}
		env := map[string]string{"DISPLAY": display}
		execReq := oapi.ProcessExecRequest{Command: "bash", Args: &args, Env: &env}
		resp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &execReq})
		if err != nil {
			return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to execute xrandr"}}, nil
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
				return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("failed to set resolution: %s", stderr)}}, nil
			}
			log.Info("resolution updated via xrandr", "display", display, "width", width, "height", height)

			// Restart Chromium to ensure it adapts to the new resolution
			log.Info("restarting chromium to adapt to new resolution")
			restartCmd := []string{"-lc", "supervisorctl restart chromium"}
			restartEnv := map[string]string{}
			restartReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartCmd, Env: &restartEnv}
			restartResp, restartErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartReq})
			if restartErr != nil {
				log.Error("failed to restart chromium after resolution change", "error", restartErr)
				// Still return success since resolution change succeeded
				// Return success with the new dimensions
				return oapi.PatchDisplay200JSONResponse{
					Width:  &width,
					Height: &height,
				}, nil
			}

			// Check if restart succeeded
			if execResp, ok := restartResp.(oapi.ProcessExec200JSONResponse); ok {
				if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
					log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
				} else {
					log.Info("chromium restarted successfully")
				}
			}

			// Return success with the new dimensions
			return oapi.PatchDisplay200JSONResponse{
				Width:  &width,
				Height: &height,
			}, nil
		case oapi.ProcessExec400JSONResponse:
			return oapi.PatchDisplay400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: r.Message}}, nil
		case oapi.ProcessExec500JSONResponse:
			return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: r.Message}}, nil
		default:
			return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "unexpected response from process exec"}}, nil
		}
	} else {
		// Xvfb path: restart with new dimensions
		log.Info("updating Xvfb resolution requires restart", "width", width, "height", height)

		// Update supervisor config to include environment variables
		// First, remove any existing environment line to avoid duplicates
		log.Info("updating xvfb supervisor config with new dimensions")
		removeEnvCmd := []string{"-lc", `sed -i '/^environment=/d' /etc/supervisor/conf.d/services/xvfb.conf`}
		removeEnvReq := oapi.ProcessExecRequest{Command: "bash", Args: &removeEnvCmd}
		s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &removeEnvReq})

		// Now add the environment line with WIDTH and HEIGHT
		addEnvCmd := []string{"-lc", fmt.Sprintf(`sed -i '/\[program:xvfb\]/a environment=WIDTH="%d",HEIGHT="%d",DPI="96",DISPLAY=":1"' /etc/supervisor/conf.d/services/xvfb.conf`, width, height)}
		addEnvReq := oapi.ProcessExecRequest{Command: "bash", Args: &addEnvCmd}
		configResp, configErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &addEnvReq})
		if configErr != nil {
			return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to update xvfb config"}}, nil
		}

		// Check if config update succeeded
		if execResp, ok := configResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				log.Error("failed to update xvfb config", "exit_code", *execResp.ExitCode)
				return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to update xvfb config"}}, nil
			}
		}

		// Reload supervisor configuration
		log.Info("reloading supervisor configuration")
		reloadCmd := []string{"-lc", "supervisorctl reread && supervisorctl update"}
		reloadReq := oapi.ProcessExecRequest{Command: "bash", Args: &reloadCmd}
		_, reloadErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &reloadReq})
		if reloadErr != nil {
			log.Error("failed to reload supervisor config", "error", reloadErr)
		}

		// Restart xvfb with new configuration
		log.Info("restarting xvfb with new resolution")
		restartXvfbCmd := []string{"-lc", "supervisorctl restart xvfb"}
		restartXvfbReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartXvfbCmd}
		xvfbResp, xvfbErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartXvfbReq})
		if xvfbErr != nil {
			return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to restart Xvfb"}}, nil
		}

		// Check if Xvfb restart succeeded
		if execResp, ok := xvfbResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				return oapi.PatchDisplay500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "Xvfb restart failed"}}, nil
			}
		}

		// Wait for Xvfb to be ready
		log.Info("waiting for Xvfb to be ready")
		waitCmd := []string{"-lc", "sleep 2"}
		waitReq := oapi.ProcessExecRequest{Command: "bash", Args: &waitCmd}
		s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &waitReq})

		// Restart Chromium
		log.Info("restarting chromium after Xvfb restart")
		restartChromeCmd := []string{"-lc", "supervisorctl restart chromium"}
		restartChromeEnv := map[string]string{}
		restartChromeReq := oapi.ProcessExecRequest{Command: "bash", Args: &restartChromeCmd, Env: &restartChromeEnv}
		chromeResp, chromeErr := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &restartChromeReq})
		if chromeErr != nil {
			log.Error("failed to restart chromium after Xvfb restart", "error", chromeErr)
			// Still return success since Xvfb restart succeeded
			// Return success with the new dimensions
			return oapi.PatchDisplay200JSONResponse{
				Width:  &width,
				Height: &height,
			}, nil
		}

		// Check if Chromium restart succeeded
		if execResp, ok := chromeResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
			} else {
				log.Info("chromium restarted successfully")
			}
		}

		log.Info("Xvfb resolution updated", "display", display, "width", width, "height", height)
		// Return success with the new dimensions
		return oapi.PatchDisplay200JSONResponse{
			Width:  &width,
			Height: &height,
		}, nil
	}
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
// It falls back to counting TCP connections if the API is unavailable.
func (s *ApiService) getActiveNekoSessions(ctx context.Context) int {
	log := logger.FromContext(ctx)

	// Create HTTP client with short timeout
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
	}

	// Query Neko API
	resp, err := client.Get("http://localhost:8080/api/sessions")
	if err != nil {
		log.Debug("failed to query Neko API, falling back to TCP counting", "error", err)
		return s.countEstablishedTCPSessions(ctx, 8080)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		log.Debug("Neko API returned non-OK status, falling back to TCP counting", "status", resp.StatusCode)
		return s.countEstablishedTCPSessions(ctx, 8080)
	}

	// Parse response
	var sessions []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		log.Debug("failed to parse Neko API response, falling back to TCP counting", "error", err)
		return s.countEstablishedTCPSessions(ctx, 8080)
	}

	log.Debug("successfully queried Neko API", "active_sessions", len(sessions))
	return len(sessions)
}

// countEstablishedTCPSessions returns the number of ESTABLISHED TCP connections for the given local port.
// This is used as a fallback when the Neko API is unavailable.
func (s *ApiService) countEstablishedTCPSessions(ctx context.Context, port int) int {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-lc", fmt.Sprintf("netstat -tn 2>/dev/null | awk '$6==\"ESTABLISHED\" && $4 ~ /:%d$/ {count++} END{print count+0}'", port))
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return 0
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return i
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

// getCurrentResolution returns the current display resolution by querying xrandr
func (s *ApiService) getCurrentResolution(ctx context.Context) (int, int) {
	log := logger.FromContext(ctx)
	display := s.resolveDisplayFromEnv()

	// Use xrandr to get current resolution
	cmd := exec.CommandContext(ctx, "bash", "-lc", "xrandr | grep -E '\\*' | awk '{print $1}'")
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", display))

	out, err := cmd.Output()
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		// Return default resolution on error
		return 1024, 768
	}

	resStr := strings.TrimSpace(string(out))
	parts := strings.Split(resStr, "x")
	if len(parts) != 2 {
		log.Error("unexpected xrandr output format", "output", resStr)
		return 1024, 768
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Error("failed to parse width", "error", err, "value", parts[0])
		return 1024, 768
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Error("failed to parse height", "error", err, "value", parts[1])
		return 1024, 768
	}

	return width, height
}

// isNekoEnabled checks if Neko service is enabled
func (s *ApiService) isNekoEnabled() bool {
	return os.Getenv("ENABLE_WEBRTC") == "true"
}

// setResolutionViaNeko delegates resolution change to Neko API
func (s *ApiService) setResolutionViaNeko(ctx context.Context, width, height int) error {
	log := logger.FromContext(ctx)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Prepare request body for Neko's screen API
	screenConfig := map[string]interface{}{
		"width":  width,
		"height": height,
		"rate":   60, // Default refresh rate
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

	// Add authentication - Neko requires admin credentials
	adminPassword := os.Getenv("NEKO_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin" // Default from neko.yaml
	}
	req.SetBasicAuth("admin", adminPassword)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Neko API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Neko API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Info("successfully changed resolution via Neko API", "width", width, "height", height)
	return nil
}
