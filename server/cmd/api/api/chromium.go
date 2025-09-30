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
	"strconv"
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

	// Parse fields: extensions[<i>].zip_file and extensions[<i>].name
	parseIndexAndField := func(name string) (int, string, bool) {
		if !strings.HasPrefix(name, "extensions") {
			return 0, "", false
		}
		if strings.HasPrefix(name, "extensions[") {
			end := strings.Index(name, "]")
			if end == -1 {
				return 0, "", false
			}
			idxStr := name[len("extensions["):end]
			rest := name[end+1:]
			rest = strings.TrimPrefix(rest, ".")
			var field string
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				field = rest[1 : len(rest)-1]
			} else {
				field = rest
			}
			idx := 0
			if v, err := strconv.Atoi(idxStr); err == nil && v >= 0 {
				idx = v
			} else {
				return 0, "", false
			}
			return idx, field, true
		}
		if strings.HasPrefix(name, "extensions.") {
			parts := strings.Split(name, ".")
			if len(parts) != 3 {
				return 0, "", false
			}
			idx := 0
			if v, err := strconv.Atoi(parts[1]); err == nil && v >= 0 {
				idx = v
			} else {
				return 0, "", false
			}
			return idx, parts[2], true
		}
		return 0, "", false
	}

	type pending struct {
		zipTemp     string
		name        string
		zipReceived bool
	}
	pendings := map[int]*pending{}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error("read form part", "err", err)
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read form part"}}, nil
		}
		idx, field, ok := parseIndexAndField(part.FormName())
		if !ok {
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid form field: " + part.FormName()}}, nil
		}
		p, exists := pendings[idx]
		if !exists {
			p = &pending{}
			pendings[idx] = p
		}
		switch field {
		case "zip_file":
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
			p.zipTemp = tmp.Name()
			p.zipReceived = true
		case "name":
			b, err := io.ReadAll(part)
			if err != nil {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to read name"}}, nil
			}
			name := strings.TrimSpace(string(b))
			if name == "" || !regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`).MatchString(name) {
				return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid extension name"}}, nil
			}
			p.name = name
		default:
			return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid field: " + field}}, nil
		}
	}

	log.Info("parsed multipart fields", "items", len(pendings))

	if len(pendings) == 0 {
		return oapi.UploadExtensionsAndRestart400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no extensions provided"}}, nil
	}

	// Materialize uploads
	extBase := "/home/kernel/extensions"

	for _, p := range pendings {
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
	for _, p := range pendings {
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
