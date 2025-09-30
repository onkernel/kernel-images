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
		log.Info("resolution updated", "display", display, "width", width, "height", height)

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
