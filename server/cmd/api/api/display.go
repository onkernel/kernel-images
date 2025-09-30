package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// DisplayStatus reports whether it is currently safe to resize the display.
// It checks for active Neko viewer sessions (approx. by counting ESTABLISHED TCP
// connections on port 8080) and whether any recording is active.
func (s *ApiService) DisplayStatus(ctx context.Context, _ oapi.DisplayStatusRequestObject) (oapi.DisplayStatusResponseObject, error) {
	live := s.countEstablishedTCPSessions(ctx, 8080)
	isRecording := s.anyRecordingActive(ctx)
	isReplaying := false // replay not currently implemented

	resizableNow := (live == 0) && !isRecording && !isReplaying

	return oapi.DisplayStatus200JSONResponse(oapi.DisplayStatus{
		LiveViewSessions: &live,
		IsRecording:      &isRecording,
		IsReplaying:      &isReplaying,
		ResizableNow:     &resizableNow,
	}), nil
}

// SetResolution safely updates the current X display resolution. When require_idle
// is true (default), it refuses to resize while live view or recording/replay is active.
// This method automatically detects whether the system is running with Xorg (headful)
// or Xvfb (headless) and uses the appropriate method to change resolution.
func (s *ApiService) SetResolution(ctx context.Context, req oapi.SetResolutionRequestObject) (oapi.SetResolutionResponseObject, error) {
	log := logger.FromContext(ctx)
	if req.Body == nil {
		return oapi.SetResolution400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "missing request body"}}, nil
	}
	width := req.Body.Width
	height := req.Body.Height
	if width <= 0 || height <= 0 {
		return oapi.SetResolution400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid width/height"}}, nil
	}
	requireIdle := true
	if req.Body.RequireIdle != nil {
		requireIdle = *req.Body.RequireIdle
	}

	// Check current status
	statusResp, _ := s.DisplayStatus(ctx, oapi.DisplayStatusRequestObject{})
	var status oapi.DisplayStatus
	switch v := statusResp.(type) {
	case oapi.DisplayStatus200JSONResponse:
		status = oapi.DisplayStatus(v)
	default:
		// In unexpected cases, default to conservative behaviour
		status = oapi.DisplayStatus{LiveViewSessions: ptrInt(0), IsRecording: ptrBool(false), IsReplaying: ptrBool(false), ResizableNow: ptrBool(true)}
	}
	if requireIdle && status.ResizableNow != nil && !*status.ResizableNow {
		return oapi.SetResolution409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "resize refused: live view or recording/replay active"}}, nil
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
			return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to execute xrandr"}}, nil
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
				return oapi.SetResolution400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("failed to set resolution: %s", stderr)}}, nil
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
				return oapi.SetResolution200JSONResponse{Ok: true}, nil
			}

			// Check if restart succeeded
			if execResp, ok := restartResp.(oapi.ProcessExec200JSONResponse); ok {
				if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
					log.Error("chromium restart failed", "exit_code", *execResp.ExitCode)
				} else {
					log.Info("chromium restarted successfully")
				}
			}

			return oapi.SetResolution200JSONResponse{Ok: true}, nil
		case oapi.ProcessExec400JSONResponse:
			return oapi.SetResolution400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: r.Message}}, nil
		case oapi.ProcessExec500JSONResponse:
			return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: r.Message}}, nil
		default:
			return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "unexpected response from process exec"}}, nil
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
			return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to update xvfb config"}}, nil
		}

		// Check if config update succeeded
		if execResp, ok := configResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				log.Error("failed to update xvfb config", "exit_code", *execResp.ExitCode)
				return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to update xvfb config"}}, nil
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
			return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to restart Xvfb"}}, nil
		}

		// Check if Xvfb restart succeeded
		if execResp, ok := xvfbResp.(oapi.ProcessExec200JSONResponse); ok {
			if execResp.ExitCode != nil && *execResp.ExitCode != 0 {
				return oapi.SetResolution500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "Xvfb restart failed"}}, nil
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
			return oapi.SetResolution200JSONResponse{Ok: true}, nil
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
		return oapi.SetResolution200JSONResponse{Ok: true}, nil
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

// countEstablishedTCPSessions returns the number of ESTABLISHED TCP connections for the given local port.
// Implementation shells out to netstat, which is present in the image (net-tools).
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

func ptrBool(v bool) *bool { return &v }
func ptrInt(v int) *int    { return &v }
