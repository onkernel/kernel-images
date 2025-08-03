package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/nrednav/cuid2"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// fsWatch represents an in-memory directory watch.
type fsWatch struct {
	path      string
	recursive bool
	events    chan oapi.FileSystemEvent
	watcher   *fsnotify.Watcher
}

// addRecursive walks the directory and registers all subdirectories when recursive=true.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}

// DownloadFile serves the requested path as an octet-stream.
func (s *ApiService) DownloadFile(ctx context.Context, req oapi.DownloadFileRequestObject) (oapi.DownloadFileResponseObject, error) {
	log := logger.FromContext(ctx)
	path := req.Params.Path
	if path == "" {
		return oapi.DownloadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "path cannot be empty"}}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return oapi.DownloadFile404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "file not found"}}, nil
		}
		log.Error("failed to open file", "err", err, "path", path)
		return oapi.DownloadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to open file"}}, nil
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		log.Error("failed to stat file", "err", err, "path", path)
		return oapi.DownloadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to stat file"}}, nil
	}

	return oapi.DownloadFile200ApplicationoctetStreamResponse{
		Body:          f,
		ContentLength: stat.Size(),
	}, nil
}

// ReadFile returns the contents of a file specified by the path param.
func (s *ApiService) ReadFile(ctx context.Context, req oapi.ReadFileRequestObject) (oapi.ReadFileResponseObject, error) {
	log := logger.FromContext(ctx)
	path := req.Params.Path
	if path == "" {
		return oapi.ReadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "path cannot be empty"}}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return oapi.ReadFile404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "file not found"}}, nil
		}
		log.Error("failed to open file", "err", err, "path", path)
		return oapi.ReadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to open file"}}, nil
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		log.Error("failed to stat file", "err", err, "path", path)
		return oapi.ReadFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to stat file"}}, nil
	}

	return oapi.ReadFile200ApplicationoctetStreamResponse{
		Body:          f,
		ContentLength: stat.Size(),
	}, nil
}

// WriteFile creates or overwrites a file with the supplied data stream.
func (s *ApiService) WriteFile(ctx context.Context, req oapi.WriteFileRequestObject) (oapi.WriteFileResponseObject, error) {
	log := logger.FromContext(ctx)
	path := req.Params.Path
	if path == "" {
		return oapi.WriteFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "path cannot be empty"}}, nil
	}
	if req.Body == nil {
		return oapi.WriteFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "empty request body"}}, nil
	}

	// create parent directories if necessary
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Error("failed to create directories", "err", err, "path", path)
		return oapi.WriteFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to create directories"}}, nil
	}

	f, err := os.Create(path)
	if err != nil {
		log.Error("failed to create file", "err", err, "path", path)
		return oapi.WriteFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to create file"}}, nil
	}
	defer f.Close()

	if _, err := io.Copy(f, req.Body); err != nil {
		log.Error("failed to write file", "err", err, "path", path)
		return oapi.WriteFile400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "failed to write data"}}, nil
	}

	return oapi.WriteFile201Response{}, nil
}

// UploadFiles stores one or more files provided in a multipart form. This implementation
// supports parts named "file" and an accompanying part named "dest_path" which must appear
// before its corresponding file part.
func (s *ApiService) UploadFiles(ctx context.Context, req oapi.UploadFilesRequestObject) (oapi.UploadFilesResponseObject, error) {
	log := logger.FromContext(ctx)
	if req.Body == nil {
		return oapi.UploadFiles400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "multipart body required"}}, nil
	}

	reader := req.Body
	var currentDest string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Error("failed to read multipart part", "err", err)
			return oapi.UploadFiles400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid multipart payload"}}, nil
		}
		switch part.FormName() {
		case "dest_path":
			data, _ := io.ReadAll(part)
			currentDest = string(data)
		case "file":
			if currentDest == "" {
				return oapi.UploadFiles400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "dest_path must precede each file part"}}, nil
			}
			if err := os.MkdirAll(filepath.Dir(currentDest), 0o755); err != nil {
				return oapi.UploadFiles400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("failed to create directories: %v", err)}}, nil
			}
			out, err := os.Create(currentDest)
			if err != nil {
				log.Error("failed to create file for upload", "err", err, "path", currentDest)
				return oapi.UploadFiles500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create file"}}, nil
			}
			if _, err := io.Copy(out, part); err != nil {
				out.Close()
				log.Error("failed to write uploaded data", "err", err, "path", currentDest)
				return oapi.UploadFiles500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to write file"}}, nil
			}
			out.Close()
			currentDest = "" // reset for next file
		default:
			// ignore unknown parts
		}
	}

	return oapi.UploadFiles201Response{}, nil
}

// StartFsWatch is not implemented in this basic filesystem handler. It returns a 400 error to the client.
func (s *ApiService) StartFsWatch(ctx context.Context, req oapi.StartFsWatchRequestObject) (oapi.StartFsWatchResponseObject, error) {
	log := logger.FromContext(ctx)
	if req.Body == nil {
		return oapi.StartFsWatch400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}

	path := req.Body.Path
	if path == "" {
		return oapi.StartFsWatch400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "path cannot be empty"}}, nil
	}
	// Ensure path exists
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return oapi.StartFsWatch404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "path not found"}}, nil
		}
		return oapi.StartFsWatch400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "unable to stat path"}}, nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error("failed to create fsnotify watcher", "err", err)
		return oapi.StartFsWatch500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
	}

	recursive := req.Body.Recursive != nil && *req.Body.Recursive
	if recursive {
		if err := addRecursive(watcher, path); err != nil {
			log.Error("failed to add directories recursively", "err", err)
			watcher.Close()
			return oapi.StartFsWatch500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
		}
	} else {
		if err := watcher.Add(path); err != nil {
			log.Error("failed to watch path", "err", err, "path", path)
			watcher.Close()
			return oapi.StartFsWatch500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
		}
	}

	watchID := cuid2.Generate()
	w := &fsWatch{
		path:      path,
		recursive: recursive,
		events:    make(chan oapi.FileSystemEvent, 100),
		watcher:   watcher,
	}

	// goroutine to forward events
	go func() {
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				var evType oapi.FileSystemEventType
				switch {
				case ev.Op&fsnotify.Create != 0:
					evType = "CREATE"
				case ev.Op&fsnotify.Write != 0:
					evType = "WRITE"
				case ev.Op&fsnotify.Remove != 0:
					evType = "DELETE"
				case ev.Op&fsnotify.Rename != 0:
					evType = "RENAME"
				default:
					continue
				}
				info, _ := os.Stat(ev.Name)
				isDir := info != nil && info.IsDir()
				name := filepath.Base(ev.Name)
				w.events <- oapi.FileSystemEvent{Type: evType, Path: ev.Name, Name: &name, IsDir: &isDir}

				// if recursive and new directory created, add watch
				if recursive && evType == "CREATE" && isDir {
					watcher.Add(ev.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error("fsnotify error", "err", err)
			}
		}
	}()

	s.watchMu.Lock()
	s.watches[watchID] = w
	s.watchMu.Unlock()

	return oapi.StartFsWatch201JSONResponse{WatchId: &watchID}, nil
}

func (s *ApiService) StopFsWatch(ctx context.Context, req oapi.StopFsWatchRequestObject) (oapi.StopFsWatchResponseObject, error) {
	log := logger.FromContext(ctx)
	id := req.WatchId
	s.watchMu.Lock()
	w, ok := s.watches[id]
	if ok {
		delete(s.watches, id)
		w.watcher.Close()
		close(w.events)
	}
	s.watchMu.Unlock()

	if !ok {
		log.Warn("stop requested for unknown watch", "watch_id", id)
		return oapi.StopFsWatch404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "watch not found"}}, nil
	}

	return oapi.StopFsWatch204Response{}, nil
}

func (s *ApiService) StreamFsEvents(ctx context.Context, req oapi.StreamFsEventsRequestObject) (oapi.StreamFsEventsResponseObject, error) {
	log := logger.FromContext(ctx)
	id := req.WatchId
	s.watchMu.RLock()
	w, ok := s.watches[id]
	s.watchMu.RUnlock()
	if !ok {
		log.Warn("stream requested for unknown watch", "watch_id", id)
		return oapi.StreamFsEvents404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "watch not found"}}, nil
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		enc := json.NewEncoder(pw)
		for ev := range w.events {
			// Write SSE formatted event: data: <json>\n\n
			pw.Write([]byte("data: "))
			if err := enc.Encode(ev); err != nil {
				log.Error("failed to encode fs event", "err", err)
				return
			}
			pw.Write([]byte("\n"))
		}
	}()

	headers := oapi.StreamFsEvents200ResponseHeaders{XSSEContentType: "application/json"}
	return oapi.StreamFsEvents200TexteventStreamResponse{Body: pr, Headers: headers, ContentLength: 0}, nil
}
