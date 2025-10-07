package api

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/onkernel/kernel-images/server/lib/chromiumflags"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/ziputil"
)

var nameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

// UploadExtensionsAndRestart handles multipart upload of one or more extension zips, extracts
// them under /home/kernel/extensions/<name>, writes /chromium/flags to enable them, restarts
// Chromium via supervisord, and waits (via UpstreamManager) until DevTools is ready.
func (s *ApiService) UploadExtensionsAndRestart(ctx context.Context, request oapi.UploadExtensionsAndRestartRequestObject) (oapi.UploadExtensionsAndRestartResponseObject, error) {
	log := logger.FromContext(ctx)
	start := time.Now()
	log.Info("upload extensions: begin")

	s.stz.Disable(ctx)
	defer s.stz.Enable(ctx)

	if request.Body == nil {
		return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}

	// Strict handler gives us *multipart.Reader; use NextPart() directly
	mr, ok := any(request.Body).(interface {
		NextPart() (*multipart.Part, error)
	})
	if !ok {
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "multipart reader not available"}}, nil
	}

	temps := []string{}
	defer func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}()

	type pending struct {
		zipTemp     string
		name        string
		zipReceived bool
	}
	// Process consecutive pairs of fields:
	//   extensions.name (text)
	//   extensions.zip_file (file)
	// Order may be name then zip or zip then name, but they must be consecutive.
	items := []pending{}
	var current *pending

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error("read form part", "error", err)
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read form part"}}, nil
		}
		if current == nil {
			current = &pending{}
		}
		switch part.FormName() {
		case "extensions.zip_file":
			tmp, err := os.CreateTemp("", "ext-*.zip")
			if err != nil {
				log.Error("failed to create temporary file", "error", err)
				return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
			}
			temps = append(temps, tmp.Name())
			if _, err := io.Copy(tmp, part); err != nil {
				tmp.Close()
				log.Error("failed to read zip file", "error", err)
				return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to read zip file"}}, nil
			}
			if err := tmp.Close(); err != nil {
				log.Error("failed to finalize temporary file", "error", err)
				return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
			}
			if current.zipReceived {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "duplicate zip_file in pair"}}, nil
			}
			current.zipTemp = tmp.Name()
			current.zipReceived = true
		case "extensions.name":
			b, err := io.ReadAll(part)
			if err != nil {
				log.Error("failed to read name", "error", err)
				return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to read name"}}, nil
			}
			name := strings.TrimSpace(string(b))
			if name == "" || !nameRegex.MatchString(name) {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid extension name"}}, nil
			}
			if current.name != "" {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "duplicate name in pair"}}, nil
			}
			current.name = name
		default:
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("invalid field: %s", part.FormName())}}, nil
		}
		// If we have both fields, finalize this item
		if current != nil && current.zipReceived && current.name != "" {
			items = append(items, *current)
			current = nil
		}
	}

	// If the last pair is incomplete, reject the request
	if current != nil && (!current.zipReceived || current.name == "") {
		return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "each extension must include consecutive name and zip_file"}}, nil
	}

	if len(items) == 0 {
		return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no extensions provided"}}, nil
	}

	// Materialize uploads
	extBase := "/home/kernel/extensions"

	// Fail early if any destination already exists
	for _, p := range items {
		dest := filepath.Join(extBase, p.name)
		if _, err := os.Stat(dest); err == nil {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("extension name already exists: %s", p.name)}}, nil
		} else if !os.IsNotExist(err) {
			log.Error("failed to check extension dir", "error", err)
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to check extension dir"}}, nil
		}
	}

	for _, p := range items {
		if !p.zipReceived || p.name == "" {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "each item must include zip_file and name"}}, nil
		}
		dest := filepath.Join(extBase, p.name)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			log.Error("failed to create extension dir", "error", err)
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create extension dir"}}, nil
		}
		if err := ziputil.Unzip(p.zipTemp, dest); err != nil {
			log.Error("failed to unzip zip file", "error", err)
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid zip file"}}, nil
		}
		if err := exec.Command("chown", "-R", "kernel:kernel", dest).Run(); err != nil {
			log.Error("failed to chown extension dir", "error", err)
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to chown extension dir"}}, nil
		}
		log.Info("installed extension", "name", p.name)
	}

	// Build flags overlay file in /chromium/flags, merging with existing flags
	var paths []string
	for _, p := range items {
		paths = append(paths, filepath.Join(extBase, p.name))
	}

	// Read existing runtime flags from /chromium/flags (if any)
	const flagsPath = "/chromium/flags"
	existingTokens, err := chromiumflags.ReadOptionalFlagFile(flagsPath)
	if err != nil {
		log.Error("failed to read existing flags", "error", err)
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to read existing flags"}}, nil
	}

	// Create new flags for the uploaded extensions
	newTokens := []string{
		fmt.Sprintf("--disable-extensions-except=%s", strings.Join(paths, ",")),
		fmt.Sprintf("--load-extension=%s", strings.Join(paths, ",")),
	}

	// Merge existing flags with new extension flags using token-aware API
	mergedTokens := chromiumflags.MergeFlags(existingTokens, newTokens)

	if err := os.MkdirAll("/chromium", 0o755); err != nil {
		log.Error("failed to create chromium dir", "error", err)
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create chromium dir"}}, nil
	}
	// Write flags file with merged flags
	if err := chromiumflags.WriteFlagFile(flagsPath, mergedTokens); err != nil {
		log.Error("failed to write overlay flags", "error", err)
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to write overlay flags"}}, nil
	}

	// Begin listening for devtools URL updates, since we are about to restart Chromium
	updates, cancelSub := s.upstreamMgr.Subscribe()
	defer cancelSub()

	// Run supervisorctl restart with a new context to let it run beyond the lifetime of the http request.
	// This lets us return as soon as the DevTools URL is updated.
	errCh := make(chan error, 1)
	log.Info("restarting chromium via supervisorctl")
	go func() {
		cmdCtx, cancelCmd := context.WithTimeout(context.WithoutCancel(ctx), 1*time.Minute)
		defer cancelCmd()
		out, err := exec.CommandContext(cmdCtx, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "restart", "chromium").CombinedOutput()
		if err != nil {
			log.Error("failed to restart chromium", "error", err, "out", string(out))
			errCh <- fmt.Errorf("supervisorctl restart failed: %w", err)
		}
	}()

	// Wait for either a new upstream, a restart error, or timeout
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	select {
	case <-updates:
		log.Info("devtools ready", "elapsed", time.Since(start).String())
		return oapi.UploadExtensionsAndRestart201Response{}, nil
	case err := <-errCh:
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: err.Error()}}, nil
	case <-timeout.C:
		log.Info("devtools not ready in time", "elapsed", time.Since(start).String())
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "devtools not ready in time"}}, nil
	}
}
