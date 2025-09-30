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

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/ziputil"
)

// UploadExtensionsAndRestart handles multipart upload of one or more extension zips, extracts
// them under /home/kernel/extensions/<name>, writes /chromium/flags to enable them, restarts
// Chromium via supervisord, and waits (via UpstreamManager) until DevTools is ready.
func (s *ApiService) UploadExtensionsAndRestart(ctx context.Context, request oapi.UploadExtensionsAndRestartRequestObject) (oapi.UploadExtensionsAndRestartResponseObject, error) {
	log := logger.FromContext(ctx)
	start := time.Now()
	log.Info("upload extensions: begin")

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
	// We now only accept consecutive pairs of fields:
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
			log.Error("read form part", "err", err)
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read form part"}}, nil
		}
		fieldName := part.FormName()
		if fieldName != "extensions.zip_file" && fieldName != "extensions.name" {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid form field: " + part.FormName()}}, nil
		}
		if current == nil {
			current = &pending{}
		}
		switch fieldName {
		case "extensions.zip_file":
			tmp, err := os.CreateTemp("", "ext-*.zip")
			if err != nil {
				return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
			}
			temps = append(temps, tmp.Name())
			if _, err := io.Copy(tmp, part); err != nil {
				tmp.Close()
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read zip file"}}, nil
			}
			if err := tmp.Close(); err != nil {
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
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read name"}}, nil
			}
			name := strings.TrimSpace(string(b))
			if name == "" || !regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`).MatchString(name) {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid extension name"}}, nil
			}
			if current.name != "" {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "duplicate name in pair"}}, nil
			}
			current.name = name
		default:
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid field"}}, nil
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

	log.Info("parsed multipart fields", "items", len(items))

	if len(items) == 0 {
		return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no extensions provided"}}, nil
	}

	// Materialize uploads
	extBase := "/home/kernel/extensions"

	for _, p := range items {
		if !p.zipReceived || p.name == "" {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "each item must include zip_file and name"}}, nil
		}
		dest := filepath.Join(extBase, p.name)
		log.Info("processing extension", "name", p.name, "dest", dest)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create extension dir"}}, nil
		}
		if err := ziputil.Unzip(p.zipTemp, dest); err != nil {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid zip file"}}, nil
		}
		if err := exec.Command("chown", "-R", "kernel:kernel", dest).Run(); err != nil {
			log.Error("failed to chown extension dir", "err", err)
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to chown extension dir"}}, nil
		}
		log.Info("installed extension", "name", p.name)
	}

	// Build flags overlay
	var paths []string
	for _, p := range items {
		paths = append(paths, filepath.Join(extBase, p.name))
	}
	overlay := fmt.Sprintf("--disable-extensions-except=%s --load-extension=%s\n", strings.Join(paths, ","), strings.Join(paths, ","))
	// Ensure /chromium exists
	if err := exec.Command("mkdir", "-p", "/chromium").Run(); err != nil {
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create chromium dir"}}, nil
	}
	if err := os.WriteFile("/chromium/flags", []byte(overlay), 0o644); err != nil {
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to write overlay flags"}}, nil
	}
	if err := os.Chmod("/chromium/flags", 0o644); err != nil {
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to chmod chromium flags"}}, nil
	}
	log.Info("wrote /chromium/flags", "paths", strings.Join(paths, ","))

	// Debug: list directories to verify ownership/permissions
	if out, err := exec.Command("ls", "-alh", "/home/kernel/extensions").CombinedOutput(); err == nil {
		log.Info("ls -alh /home/kernel/extensions", "out", string(out))
	} else {
		log.Info("ls -alh /home/kernel/extensions failed", "err", err.Error(), "out", string(out))
	}
	if out, err := exec.Command("ls", "-alh", "/chromium").CombinedOutput(); err == nil {
		log.Info("ls -alh /chromium", "out", string(out))
	} else {
		log.Info("ls -alh /chromium failed", "err", err.Error(), "out", string(out))
	}

	// Subscribe to upstream updates BEFORE triggering restart to avoid races
	updates, cancelSub := s.upstreamMgr.Subscribe()
	defer cancelSub()
	log.Info("subscribed to upstream updates")

	// Fire-and-forget supervisorctl restart in a background goroutine.
	// Capture first error if it happens; do not block returning success if upstream is ready earlier.
	errCh := make(chan error, 1)
	log.Info("restarting chromium via supervisorctl")
	go func() {
		out, err := exec.Command("supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "restart", "chromium").CombinedOutput()
		if err != nil {
			log.Error("failed to restart chromium", "err", err, "out", string(out))
			errCh <- fmt.Errorf("supervisorctl restart failed: %w", err)
			return
		}
		// signal success by closing channel if no error was sent
		close(errCh)
	}()

	// Wait for either a new upstream, a restart error, or timeout
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	select {
	case <-updates:
		log.Info("devtools ready", "elapsed", time.Since(start).String())
		return oapi.UploadExtensionsAndRestart201Response{}, nil
	case err := <-errCh:
		if err != nil {
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: err.Error()}}, nil
		}
		select {
		case <-updates:
			log.Info("devtools ready (after restart completes)", "elapsed", time.Since(start).String())
			return oapi.UploadExtensionsAndRestart201Response{}, nil
		case <-timeout.C:
			log.Info("devtools not ready in time (post-restart)", "elapsed", time.Since(start).String())
			return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "devtools not ready in time"}}, nil
		}
	case <-timeout.C:
		log.Info("devtools not ready in time", "elapsed", time.Since(start).String())
		return oapi.UploadExtensionsAndRestart500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "devtools not ready in time"}}, nil
	}
}
