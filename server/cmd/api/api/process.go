package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

type processHandle struct {
	id       openapi_types.UUID
	pid      int
	cmd      *exec.Cmd
	started  time.Time
	exitCode *int
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	outCh    chan oapi.ProcessStreamEvent
	doneCh   chan struct{}
	mu       sync.RWMutex
}

func (h *processHandle) state() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.exitCode != nil {
		return "exited"
	}
	return "running"
}

func (h *processHandle) setExited(code int) {
	h.mu.Lock()
	if h.exitCode == nil {
		h.exitCode = &code
	}
	h.mu.Unlock()
}

func buildCmd(body *oapi.ProcessExecRequest) (*exec.Cmd, error) {
	if body == nil || body.Command == "" {
		return nil, errors.New("command required")
	}
	var args []string
	if body.Args != nil {
		args = append(args, (*body.Args)...)
	}
	cmd := exec.Command(body.Command, args...)
	if body.Cwd != nil && *body.Cwd != "" {
		cmd.Dir = *body.Cwd
		// Ensure absolute if provided
		if !filepath.IsAbs(cmd.Dir) {
			// make relative to current working directory
			wd, _ := os.Getwd()
			cmd.Dir = filepath.Join(wd, cmd.Dir)
		}
	}
	// Build environment
	envMap := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			envMap[kv[:i]] = kv[i+1:]
		}
	}
	if body.Env != nil {
		for k, v := range *body.Env {
			envMap[k] = v
		}
	}
	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	// Configure user if requested
	if body.AsRoot != nil && *body.AsRoot && body.AsUser != nil && *body.AsUser != "" {
		return nil, errors.New("cannot specify both as_root and as_user")
	}
	if body.AsRoot != nil && *body.AsRoot {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: 0, Gid: 0},
		}
	} else if body.AsUser != nil && *body.AsUser != "" {
		spec := *body.AsUser
		// support forms: "username" or "uid" or "uid:gid"
		var uidStr, gidStr string
		if i := strings.IndexByte(spec, ':'); i >= 0 {
			uidStr = spec[:i]
			gidStr = spec[i+1:]
		} else {
			uidStr = spec
		}

		var u *user.User
		var err error
		if _, errNum := strconv.Atoi(uidStr); errNum == nil {
			u, err = user.LookupId(uidStr)
		} else {
			u, err = user.Lookup(uidStr)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to lookup user %q: %w", spec, err)
		}
		uid64, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid uid for user %q: %w", spec, err)
		}
		gid64, err := strconv.ParseUint(u.Gid, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid gid for user %q: %w", spec, err)
		}
		// If gid override provided, require it to be numeric
		if gidStr != "" {
			if gOverride, err := strconv.ParseUint(gidStr, 10, 32); err == nil {
				gid64 = gOverride
			} else {
				return nil, fmt.Errorf("gid override must be numeric, got %q", gidStr)
			}
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(uid64), Gid: uint32(gid64)},
		}
	}
	return cmd, nil
}

// Execute a command synchronously
// (POST /process/exec)
func (s *ApiService) ProcessExec(ctx context.Context, request oapi.ProcessExecRequestObject) (oapi.ProcessExecResponseObject, error) {
	log := logger.FromContext(ctx)
	if request.Body == nil {
		return oapi.ProcessExec400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}

	cmd, err := buildCmd((*oapi.ProcessExecRequest)(request.Body))
	if err != nil {
		return oapi.ProcessExec400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: err.Error()}}, nil
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Handle timeout if provided
	start := time.Now()
	var cancel context.CancelFunc
	if request.Body.TimeoutSec != nil && *request.Body.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*request.Body.TimeoutSec)*time.Second)
		defer cancel()
	}
	if err := cmd.Start(); err != nil {
		log.Error("failed to start process", "err", err)
		return oapi.ProcessExec500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start process"}}, nil
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done // ensure wait returns
		return oapi.ProcessExec500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "process timed out"}}, nil
	case err := <-done:
		// proceed
		_ = err
	}
	durationMs := int(time.Since(start) / time.Millisecond)
	exitCode := 0
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			exitCode = status.ExitStatus()
		}
	}

	resp := oapi.ProcessExec200JSONResponse{
		ExitCode:   &exitCode,
		StdoutB64:  ptrOf(base64.StdEncoding.EncodeToString(stdoutBuf.Bytes())),
		StderrB64:  ptrOf(base64.StdEncoding.EncodeToString(stderrBuf.Bytes())),
		DurationMs: &durationMs,
	}
	return resp, nil
}

// Execute a command asynchronously
// (POST /process/spawn)
func (s *ApiService) ProcessSpawn(ctx context.Context, request oapi.ProcessSpawnRequestObject) (oapi.ProcessSpawnResponseObject, error) {
	log := logger.FromContext(ctx)
	if request.Body == nil {
		return oapi.ProcessSpawn400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}
	// Build from ProcessExecRequest shape
	execReq := oapi.ProcessExecRequest{
		Command:    request.Body.Command,
		Args:       request.Body.Args,
		Cwd:        request.Body.Cwd,
		Env:        request.Body.Env,
		AsUser:     request.Body.AsUser,
		AsRoot:     request.Body.AsRoot,
		TimeoutSec: request.Body.TimeoutSec,
	}
	cmd, err := buildCmd(&execReq)
	if err != nil {
		return oapi.ProcessSpawn400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: err.Error()}}, nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return oapi.ProcessSpawn500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to open stdout"}}, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return oapi.ProcessSpawn500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to open stderr"}}, nil
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return oapi.ProcessSpawn500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to open stdin"}}, nil
	}
	if err := cmd.Start(); err != nil {
		log.Error("failed to start process", "err", err)
		return oapi.ProcessSpawn500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start process"}}, nil
	}

	// Disable scale-to-zero while the process is running.
	// Track success so we only re-enable if disable succeeded.
	stzDisabled := false
	if err := s.stz.Disable(ctx); err != nil {
		log.Error("failed to disable scale-to-zero", "err", err)
	} else {
		stzDisabled = true
	}

	id := openapi_types.UUID(uuid.New())
	h := &processHandle{
		id:      id,
		pid:     cmd.Process.Pid,
		cmd:     cmd,
		started: time.Now(),
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		outCh:   make(chan oapi.ProcessStreamEvent, 256),
		doneCh:  make(chan struct{}),
	}

	// Store handle
	s.procMu.Lock()
	if s.procs == nil {
		s.procs = make(map[string]*processHandle)
	}
	s.procs[id.String()] = h
	s.procMu.Unlock()

	// Reader goroutines
	go func() {
		reader := bufio.NewReader(stdout)
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				data := base64.StdEncoding.EncodeToString(buf[:n])
				stream := oapi.ProcessStreamEventStream("stdout")
				h.outCh <- oapi.ProcessStreamEvent{Stream: &stream, DataB64: &data}
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		reader := bufio.NewReader(stderr)
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				data := base64.StdEncoding.EncodeToString(buf[:n])
				stream := oapi.ProcessStreamEventStream("stderr")
				h.outCh <- oapi.ProcessStreamEvent{Stream: &stream, DataB64: &data}
			}
			if err != nil {
				break
			}
		}
	}()

	// Waiter goroutine - use context without cancel since HTTP request may complete
	// before the process exits
	stzCtx := context.WithoutCancel(ctx)
	go func(stzWasDisabled bool) {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					code = status.ExitStatus()
				}
			}
		} else if cmd.ProcessState != nil {
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
				code = status.ExitStatus()
			}
		}
		h.setExited(code)

		// Re-enable scale-to-zero now that the process has exited,
		// but only if we successfully disabled it earlier
		if stzWasDisabled {
			if err := s.stz.Enable(stzCtx); err != nil {
				log.Error("failed to enable scale-to-zero", "err", err)
			}
		}

		// Send exit event
		evt := oapi.ProcessStreamEventEvent("exit")
		h.outCh <- oapi.ProcessStreamEvent{Event: &evt, ExitCode: &code}
		close(h.doneCh)
		// Retain the handle for a short period so clients can observe the
		// final "exited" status via ProcessStatus before it disappears.
		// This avoids races where the process exits immediately after spawn
		// and status polling returns 404.
		retention := 10 * time.Second
		go func(procID string) {
			time.Sleep(retention)
			s.procMu.Lock()
			delete(s.procs, procID)
			s.procMu.Unlock()
		}(id.String())
	}(stzDisabled)

	startedAt := h.started
	pid := h.pid
	return oapi.ProcessSpawn200JSONResponse{
		ProcessId: &id,
		Pid:       &pid,
		StartedAt: &startedAt,
	}, nil
}

// Send signal to process
// (POST /process/{process_id}/kill)
func (s *ApiService) ProcessKill(ctx context.Context, request oapi.ProcessKillRequestObject) (oapi.ProcessKillResponseObject, error) {
	log := logger.FromContext(ctx)
	id := request.ProcessId.String()
	s.procMu.RLock()
	h, ok := s.procs[id]
	s.procMu.RUnlock()
	if !ok {
		return oapi.ProcessKill404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "process not found"}}, nil
	}
	if request.Body == nil {
		return oapi.ProcessKill400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}
	// Map signal
	var sig syscall.Signal
	switch request.Body.Signal {
	case "TERM":
		sig = syscall.SIGTERM
	case "KILL":
		sig = syscall.SIGKILL
	case "INT":
		sig = syscall.SIGINT
	case "HUP":
		sig = syscall.SIGHUP
	default:
		return oapi.ProcessKill400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid signal"}}, nil
	}
	if h.cmd.Process == nil {
		return oapi.ProcessKill404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "process not running"}}, nil
	}
	if err := h.cmd.Process.Signal(sig); err != nil {
		log.Error("failed to signal process", "err", err)
		return oapi.ProcessKill500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to signal process"}}, nil
	}
	return oapi.ProcessKill200JSONResponse(oapi.OkResponse{Ok: true}), nil
}

// Get process status
// (GET /process/{process_id}/status)
func (s *ApiService) ProcessStatus(ctx context.Context, request oapi.ProcessStatusRequestObject) (oapi.ProcessStatusResponseObject, error) {
	id := request.ProcessId.String()
	s.procMu.RLock()
	h, ok := s.procs[id]
	s.procMu.RUnlock()
	if !ok {
		return oapi.ProcessStatus404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "process not found"}}, nil
	}
	stateStr := h.state()
	state := oapi.ProcessStatusState(stateStr)
	var exitCode *int
	h.mu.RLock()
	if h.exitCode != nil {
		v := *h.exitCode
		exitCode = &v
	}
	pid := h.pid
	h.mu.RUnlock()
	// Best-effort memory stats via /proc
	var memBytes int
	if stateStr == "running" && pid > 0 {
		if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status"); err == nil {
			// Parse VmRSS:   123 kB
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if v, err := strconv.Atoi(fields[1]); err == nil {
							// fields[2] is likely kB
							memBytes = v * 1024
						}
					}
					break
				}
			}
		}
	}
	cpuPct := float32(0)
	resp := oapi.ProcessStatus200JSONResponse{State: &state, ExitCode: exitCode, CpuPct: &cpuPct}
	if memBytes > 0 {
		resp.MemBytes = ptrOf(memBytes)
	}
	return resp, nil
}

// Write to process stdin
// (POST /process/{process_id}/stdin)
func (s *ApiService) ProcessStdin(ctx context.Context, request oapi.ProcessStdinRequestObject) (oapi.ProcessStdinResponseObject, error) {
	id := request.ProcessId.String()
	s.procMu.RLock()
	h, ok := s.procs[id]
	s.procMu.RUnlock()
	if !ok {
		return oapi.ProcessStdin404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "process not found"}}, nil
	}
	if request.Body == nil {
		return oapi.ProcessStdin400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}
	data, err := base64.StdEncoding.DecodeString(request.Body.DataB64)
	if err != nil {
		return oapi.ProcessStdin400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid base64"}}, nil
	}
	n, err := h.stdin.Write(data)
	if err != nil {
		return oapi.ProcessStdin500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to write to stdin"}}, nil
	}
	return oapi.ProcessStdin200JSONResponse{WrittenBytes: ptrOf(n)}, nil
}

// Stream process stdout/stderr (SSE)
// (GET /process/{process_id}/stdout/stream)
func (s *ApiService) ProcessStdoutStream(ctx context.Context, request oapi.ProcessStdoutStreamRequestObject) (oapi.ProcessStdoutStreamResponseObject, error) {
	log := logger.FromContext(ctx)
	id := request.ProcessId.String()
	s.procMu.RLock()
	h, ok := s.procs[id]
	s.procMu.RUnlock()
	if !ok {
		return oapi.ProcessStdoutStream404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "process not found"}}, nil
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for {
			select {
			case evt := <-h.outCh:
				// Write SSE: data: <json>\n\n
				var buf bytes.Buffer
				if err := json.NewEncoder(&buf).Encode(evt); err != nil {
					log.Error("failed to marshal event", "err", err)
					return
				}
				line := bytes.TrimRight(buf.Bytes(), "\n")
				if _, err := pw.Write([]byte("data: ")); err != nil {
					return
				}
				if _, err := pw.Write(line); err != nil {
					return
				}
				if _, err := pw.Write([]byte("\n\n")); err != nil {
					return
				}
			case <-h.doneCh:
				return
			}
		}
	}()

	headers := oapi.ProcessStdoutStream200ResponseHeaders{XSSEContentType: "application/json"}
	return oapi.ProcessStdoutStream200TexteventStreamResponse{Body: pr, Headers: headers, ContentLength: 0}, nil
}

func ptrOf[T any](v T) *T { return &v }
