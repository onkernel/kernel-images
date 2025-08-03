package api

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// TestWriteReadDownloadFile verifies that files can be written, read back, and downloaded successfully.
func TestWriteReadDownloadFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := &ApiService{defaultRecorderID: "default"}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := "hello world"

	// Write the file
	if resp, err := svc.WriteFile(ctx, oapi.WriteFileRequestObject{
		Params: oapi.WriteFileParams{Path: filePath},
		Body:   strings.NewReader(content),
	}); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	} else {
		if _, ok := resp.(oapi.WriteFile201Response); !ok {
			t.Fatalf("unexpected response type from WriteFile: %T", resp)
		}
	}

	// Read the file
	readResp, err := svc.ReadFile(ctx, oapi.ReadFileRequestObject{Params: oapi.ReadFileParams{Path: filePath}})
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	r200, ok := readResp.(oapi.ReadFile200ApplicationoctetStreamResponse)
	if !ok {
		t.Fatalf("unexpected response type from ReadFile: %T", readResp)
	}
	data, _ := io.ReadAll(r200.Body)
	if got := string(data); got != content {
		t.Fatalf("ReadFile content mismatch: got %q want %q", got, content)
	}

	// Download the file
	dlResp, err := svc.DownloadFile(ctx, oapi.DownloadFileRequestObject{Params: oapi.DownloadFileParams{Path: filePath}})
	if err != nil {
		t.Fatalf("DownloadFile returned error: %v", err)
	}
	d200, ok := dlResp.(oapi.DownloadFile200ApplicationoctetStreamResponse)
	if !ok {
		t.Fatalf("unexpected response type from DownloadFile: %T", dlResp)
	}
	dlData, _ := io.ReadAll(d200.Body)
	if got := string(dlData); got != content {
		t.Fatalf("DownloadFile content mismatch: got %q want %q", got, content)
	}

	// Attempt to read non-existent file
	missingResp, err := svc.ReadFile(ctx, oapi.ReadFileRequestObject{Params: oapi.ReadFileParams{Path: filepath.Join(tmpDir, "missing.txt")}})
	if err != nil {
		t.Fatalf("ReadFile missing file returned error: %v", err)
	}
	if _, ok := missingResp.(oapi.ReadFile404JSONResponse); !ok {
		t.Fatalf("expected 404 response for missing file, got %T", missingResp)
	}

	// Attempt to write with empty path
	badResp, err := svc.WriteFile(ctx, oapi.WriteFileRequestObject{Params: oapi.WriteFileParams{Path: ""}, Body: strings.NewReader("data")})
	if err != nil {
		t.Fatalf("WriteFile bad path returned error: %v", err)
	}
	if _, ok := badResp.(oapi.WriteFile400JSONResponse); !ok {
		t.Fatalf("expected 400 response for empty path, got %T", badResp)
	}
}

// TestUploadFiles verifies multipart upload and filesystem watch event generation.
func TestUploadFilesAndWatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := &ApiService{defaultRecorderID: "default", watches: make(map[string]*fsWatch)}

	// Prepare watch
	dir := t.TempDir()
	recursive := true
	startReq := oapi.StartFsWatchRequestObject{Body: &oapi.StartFsWatchRequest{Path: dir, Recursive: &recursive}}
	startResp, err := svc.StartFsWatch(ctx, startReq)
	if err != nil {
		t.Fatalf("StartFsWatch error: %v", err)
	}
	sr201, ok := startResp.(oapi.StartFsWatch201JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type from StartFsWatch: %T", startResp)
	}
	if sr201.WatchId == nil {
		t.Fatalf("watch id nil")
	}
	watchID := *sr201.WatchId

	// Build multipart payload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	destPath := filepath.Join(dir, "upload.txt")

	// dest_path part
	if err := mw.WriteField("dest_path", destPath); err != nil {
		t.Fatalf("WriteField error: %v", err)
	}
	// file part
	fw, err := mw.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	content := "upload content"
	fw.Write([]byte(content))
	mw.Close()

	uploadReq := oapi.UploadFilesRequestObject{Body: multipart.NewReader(&buf, mw.Boundary())}
	if resp, err := svc.UploadFiles(ctx, uploadReq); err != nil {
		t.Fatalf("UploadFiles error: %v", err)
	} else {
		if _, ok := resp.(oapi.UploadFiles201Response); !ok {
			t.Fatalf("unexpected response type from UploadFiles: %T", resp)
		}
	}

	// Verify file exists
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != content {
		t.Fatalf("uploaded file mismatch: %v", err)
	}

	// Stream events (should at least receive one)
	streamReq := oapi.StreamFsEventsRequestObject{WatchId: watchID}
	streamResp, err := svc.StreamFsEvents(ctx, streamReq)
	if err != nil {
		t.Fatalf("StreamFsEvents error: %v", err)
	}
	st200, ok := streamResp.(oapi.StreamFsEvents200TexteventStreamResponse)
	if !ok {
		t.Fatalf("unexpected response type from StreamFsEvents: %T", streamResp)
	}

	reader := bufio.NewReader(st200.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read SSE line: %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("unexpected SSE format: %s", line)
	}

	// Cleanup
	stopResp, err := svc.StopFsWatch(ctx, oapi.StopFsWatchRequestObject{WatchId: watchID})
	if err != nil {
		t.Fatalf("StopFsWatch error: %v", err)
	}
	if _, ok := stopResp.(oapi.StopFsWatch204Response); !ok {
		t.Fatalf("unexpected response type from StopFsWatch: %T", stopResp)
	}
}
