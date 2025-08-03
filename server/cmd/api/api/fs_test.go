package api

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// TestWriteReadFile verifies that files can be written and read back successfully.
func TestWriteReadFile(t *testing.T) {
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

	// (Download functionality removed)

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

// TestWriteFileAndWatch verifies WriteFile operation and filesystem watch event generation.
func TestWriteFileAndWatch(t *testing.T) {
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

	destPath := filepath.Join(dir, "write.txt")
	content := "write content"

	// Perform WriteFile to trigger watch events
	if resp, err := svc.WriteFile(ctx, oapi.WriteFileRequestObject{
		Params: oapi.WriteFileParams{Path: destPath},
		Body:   strings.NewReader(content),
	}); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	} else {
		if _, ok := resp.(oapi.WriteFile201Response); !ok {
			t.Fatalf("unexpected response type from WriteFile: %T", resp)
		}
	}

	// Verify file exists
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != content {
		t.Fatalf("written file mismatch: %v", err)
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

// TestFileDirOperations covers the new filesystem management endpoints.
func TestFileDirOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := &ApiService{}

	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "mydir")

	// Create directory
	modeStr := "755"
	createReq := oapi.CreateDirectoryRequestObject{Body: &oapi.CreateDirectoryRequest{Path: dirPath, Mode: &modeStr}}
	if resp, err := svc.CreateDirectory(ctx, createReq); err != nil {
		t.Fatalf("CreateDirectory error: %v", err)
	} else {
		if _, ok := resp.(oapi.CreateDirectory201Response); !ok {
			t.Fatalf("unexpected response type from CreateDirectory: %T", resp)
		}
	}

	// Write a file inside the directory
	filePath := filepath.Join(dirPath, "file.txt")
	content := "data"
	if resp, err := svc.WriteFile(ctx, oapi.WriteFileRequestObject{Params: oapi.WriteFileParams{Path: filePath}, Body: strings.NewReader(content)}); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	} else if _, ok := resp.(oapi.WriteFile201Response); !ok {
		t.Fatalf("unexpected WriteFile resp type: %T", resp)
	}

	// List files
	listResp, err := svc.ListFiles(ctx, oapi.ListFilesRequestObject{Params: oapi.ListFilesParams{Path: dirPath}})
	if err != nil {
		t.Fatalf("ListFiles error: %v", err)
	}
	lf200, ok := listResp.(oapi.ListFiles200JSONResponse)
	if !ok {
		t.Fatalf("unexpected ListFiles resp type: %T", listResp)
	}
	if len(lf200) != 1 || lf200[0].Name != "file.txt" {
		t.Fatalf("ListFiles unexpected content: %+v", lf200)
	}

	// FileInfo
	fiResp, err := svc.FileInfo(ctx, oapi.FileInfoRequestObject{Params: oapi.FileInfoParams{Path: filePath}})
	if err != nil {
		t.Fatalf("FileInfo error: %v", err)
	}
	fi200, ok := fiResp.(oapi.FileInfo200JSONResponse)
	if !ok {
		t.Fatalf("unexpected FileInfo resp: %T", fiResp)
	}
	if fi200.Name != "file.txt" || fi200.SizeBytes == 0 {
		t.Fatalf("FileInfo unexpected: %+v", fi200)
	}

	// Move file
	newFilePath := filepath.Join(dirPath, "moved.txt")
	moveReq := oapi.MovePathRequestObject{Body: &oapi.MovePathRequest{SrcPath: filePath, DestPath: newFilePath}}
	if resp, err := svc.MovePath(ctx, moveReq); err != nil {
		t.Fatalf("MovePath error: %v", err)
	} else if _, ok := resp.(oapi.MovePath200Response); !ok {
		t.Fatalf("unexpected MovePath resp type: %T", resp)
	}

	// Set permissions
	chmodReq := oapi.SetFilePermissionsRequestObject{Body: &oapi.SetFilePermissionsRequest{Path: newFilePath, Mode: "600"}}
	if resp, err := svc.SetFilePermissions(ctx, chmodReq); err != nil {
		t.Fatalf("SetFilePermissions error: %v", err)
	} else if _, ok := resp.(oapi.SetFilePermissions200Response); !ok {
		t.Fatalf("unexpected SetFilePermissions resp: %T", resp)
	}

	// Delete file
	if resp, err := svc.DeleteFile(ctx, oapi.DeleteFileRequestObject{Body: &oapi.DeletePathRequest{Path: newFilePath}}); err != nil {
		t.Fatalf("DeleteFile error: %v", err)
	} else if _, ok := resp.(oapi.DeleteFile200Response); !ok {
		t.Fatalf("unexpected DeleteFile resp: %T", resp)
	}

	// Delete directory
	if resp, err := svc.DeleteDirectory(ctx, oapi.DeleteDirectoryRequestObject{Body: &oapi.DeletePathRequest{Path: dirPath}}); err != nil {
		t.Fatalf("DeleteDirectory error: %v", err)
	} else if _, ok := resp.(oapi.DeleteDirectory200Response); !ok {
		t.Fatalf("unexpected DeleteDirectory resp: %T", resp)
	}
}
