package api

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func TestProcessExec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{procs: make(map[string]*processHandle)}

	cmd := "sh"
	args := []string{"-c", "echo -n out; echo -n err 1>&2; exit 3"}
	body := &oapi.ProcessExecRequest{Command: cmd, Args: &args}
	resp, err := svc.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: body})
	if err != nil {
		t.Fatalf("ProcessExec error: %v", err)
	}
	r200, ok := resp.(oapi.ProcessExec200JSONResponse)
	if !ok {
		t.Fatalf("unexpected resp type: %T", resp)
	}
	if r200.ExitCode == nil || *r200.ExitCode != 3 {
		t.Fatalf("exit code mismatch: %+v", r200.ExitCode)
	}
	if r200.StdoutB64 == nil || r200.StderrB64 == nil {
		t.Fatalf("missing stdout/stderr in response")
	}
	out, _ := base64.StdEncoding.DecodeString(*r200.StdoutB64)
	errB, _ := base64.StdEncoding.DecodeString(*r200.StderrB64)
	if string(out) != "out" || string(errB) != "err" {
		t.Fatalf("stdout/stderr mismatch: %q %q", string(out), string(errB))
	}
}

func TestProcessSpawnStatusAndStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{procs: make(map[string]*processHandle)}

	// Spawn a short-lived process that emits stdout and stderr then exits
	cmd := "sh"
	args := []string{"-c", "printf ABC; sleep 0.05; printf DEF 1>&2; sleep 0.05; exit 0"}
	body := &oapi.ProcessSpawnRequest{Command: cmd, Args: &args}
	spawnResp, err := svc.ProcessSpawn(ctx, oapi.ProcessSpawnRequestObject{Body: body})
	if err != nil {
		t.Fatalf("ProcessSpawn error: %v", err)
	}
	s200, ok := spawnResp.(oapi.ProcessSpawn200JSONResponse)
	if !ok || s200.ProcessId == nil || s200.Pid == nil {
		t.Fatalf("unexpected spawn resp: %+v", spawnResp)
	}

	// Status should be running initially (may race to exited; tolerate both by not asserting)
	statusResp, err := svc.ProcessStatus(ctx, oapi.ProcessStatusRequestObject{ProcessId: *s200.ProcessId})
	if err != nil {
		t.Fatalf("ProcessStatus error: %v", err)
	}
	if _, ok := statusResp.(oapi.ProcessStatus200JSONResponse); !ok {
		t.Fatalf("unexpected status resp: %T", statusResp)
	}

	// Start stream reader and collect at least two data events and one exit event
	streamResp, err := svc.ProcessStdoutStream(ctx, oapi.ProcessStdoutStreamRequestObject{ProcessId: *s200.ProcessId})
	if err != nil {
		t.Fatalf("StdoutStream error: %v", err)
	}
	st200, ok := streamResp.(oapi.ProcessStdoutStream200TexteventStreamResponse)
	if !ok {
		t.Fatalf("unexpected stream resp: %T", streamResp)
	}

	reader := bufio.NewReader(st200.Body)
	var gotStdout, gotStderr, gotExit bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !(gotStdout && gotStderr && gotExit) {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read SSE line: %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		var evt oapi.ProcessStreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if evt.Stream != nil && *evt.Stream == "stdout" && evt.DataB64 != nil {
			b, _ := base64.StdEncoding.DecodeString(*evt.DataB64)
			if strings.Contains(string(b), "ABC") {
				gotStdout = true
			}
		}
		if evt.Stream != nil && *evt.Stream == "stderr" && evt.DataB64 != nil {
			b, _ := base64.StdEncoding.DecodeString(*evt.DataB64)
			if strings.Contains(string(b), "DEF") {
				gotStderr = true
			}
		}
		if evt.Event != nil && *evt.Event == "exit" {
			gotExit = true
		}
		// consume blank line
		_, _ = reader.ReadString('\n')
	}
	if !(gotStdout && gotStderr && gotExit) {
		t.Fatalf("missing events: stdout=%v stderr=%v exit=%v", gotStdout, gotStderr, gotExit)
	}
}

func TestProcessStdinAndExit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{procs: make(map[string]*processHandle)}

	// Spawn a process that reads exactly 3 bytes then exits
	cmd := "sh"
	args := []string{"-c", "dd of=/dev/null bs=1 count=3 status=none"}
	body := &oapi.ProcessSpawnRequest{Command: cmd, Args: &args}
	spawnResp, err := svc.ProcessSpawn(ctx, oapi.ProcessSpawnRequestObject{Body: body})
	if err != nil {
		t.Fatalf("ProcessSpawn error: %v", err)
	}
	s200, ok := spawnResp.(oapi.ProcessSpawn200JSONResponse)
	if !ok || s200.ProcessId == nil {
		t.Fatalf("unexpected spawn resp: %T", spawnResp)
	}

	// Write 3 bytes
	data := base64.StdEncoding.EncodeToString([]byte("xyz"))
	stdinResp, err := svc.ProcessStdin(ctx, oapi.ProcessStdinRequestObject{ProcessId: *s200.ProcessId, Body: &oapi.ProcessStdinRequest{DataB64: data}})
	if err != nil {
		t.Fatalf("ProcessStdin error: %v", err)
	}
	st200, ok := stdinResp.(oapi.ProcessStdin200JSONResponse)
	if !ok || st200.WrittenBytes == nil || *st200.WrittenBytes != 3 {
		t.Fatalf("unexpected stdin resp: %+v", stdinResp)
	}

	// Wait for exit via status polling
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := svc.ProcessStatus(ctx, oapi.ProcessStatusRequestObject{ProcessId: *s200.ProcessId})
		if err != nil {
			t.Fatalf("ProcessStatus error: %v", err)
		}
		sr, ok := resp.(oapi.ProcessStatus200JSONResponse)
		if !ok {
			t.Fatalf("unexpected status resp: %T", resp)
		}
		if sr.State != nil && *sr.State == "exited" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process did not exit in time")
}

func TestProcessKill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{procs: make(map[string]*processHandle)}

	cmd := "sh"
	args := []string{"-c", "sleep 5"}
	body := &oapi.ProcessSpawnRequest{Command: cmd, Args: &args}
	spawnResp, err := svc.ProcessSpawn(ctx, oapi.ProcessSpawnRequestObject{Body: body})
	if err != nil {
		t.Fatalf("ProcessSpawn error: %v", err)
	}
	s200, ok := spawnResp.(oapi.ProcessSpawn200JSONResponse)
	if !ok || s200.ProcessId == nil {
		t.Fatalf("unexpected spawn resp: %T", spawnResp)
	}

	// Send KILL
	killBody := &oapi.ProcessKillRequest{Signal: "KILL"}
	killResp, err := svc.ProcessKill(ctx, oapi.ProcessKillRequestObject{ProcessId: *s200.ProcessId, Body: killBody})
	if err != nil {
		t.Fatalf("ProcessKill error: %v", err)
	}
	if _, ok := killResp.(oapi.ProcessKill200JSONResponse); !ok {
		t.Fatalf("unexpected kill resp: %T", killResp)
	}

	// Verify exited
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := svc.ProcessStatus(ctx, oapi.ProcessStatusRequestObject{ProcessId: *s200.ProcessId})
		if err != nil {
			t.Fatalf("ProcessStatus error: %v", err)
		}
		sr, ok := resp.(oapi.ProcessStatus200JSONResponse)
		if !ok {
			t.Fatalf("unexpected status resp: %T", resp)
		}
		if sr.State != nil && *sr.State == "exited" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process not killed in time")
}

func TestProcessNotFoundRoutes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{procs: make(map[string]*processHandle)}

	// random id that will not exist
	id := openapi_types.UUID(uuid.New())
	if resp, _ := svc.ProcessStatus(ctx, oapi.ProcessStatusRequestObject{ProcessId: id}); resp == nil {
		t.Fatalf("expected a response")
	} else if _, ok := resp.(oapi.ProcessStatus404JSONResponse); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
	if resp, _ := svc.ProcessStdoutStream(ctx, oapi.ProcessStdoutStreamRequestObject{ProcessId: id}); resp == nil {
		t.Fatalf("expected a response")
	} else if _, ok := resp.(oapi.ProcessStdoutStream404JSONResponse); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
}
