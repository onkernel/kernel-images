package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
)

type ApiService struct {
	// defaultRecorderID is used whenever the caller doesn't specify an explicit ID.
	defaultRecorderID string

	recordManager recorder.RecordManager
	factory       recorder.FFmpegRecorderFactory
	// Filesystem watch management
	watchMu sync.RWMutex
	watches map[string]*fsWatch

	// Process management
	procMu sync.RWMutex
	procs  map[string]*processHandle
}

// We're extending the StrictServerInterface to include our new endpoint
var _ oapi.StrictServerInterface = (*ApiService)(nil)

// SetScreenResolution endpoint
// (GET /screen/resolution)
// IsWebSocketAvailable checks if a WebSocket connection can be established to the given URL
func isWebSocketAvailable(wsURL string) bool {
	// First check if we can establish a TCP connection by parsing the URL
	u, err := url.Parse(wsURL)
	if err != nil {
		return false
	}

	// Get host and port
	host := u.Host
	if !strings.Contains(host, ":") {
		// Add default port based on scheme
		if u.Scheme == "ws" {
			host = host + ":80"
		} else if u.Scheme == "wss" {
			host = host + ":443"
		}
	}

	// Try TCP connection
	conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	// Try WebSocket connection
	dialer := websocket.Dialer{
		HandshakeTimeout: 200 * time.Millisecond,
	}

	wsConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return false
	}
	defer wsConn.Close()

	return true
}

// GetWebSocketURL determines the appropriate WebSocket URL from an HTTP request
// It can be used in tests
func getWebSocketURL(r *http.Request) string {
	// Auth parameters for WS connection
	authParams := "?password=admin&username=kernel"

	// Default local development URL - will try only in local dev
	localDevURL := "ws://localhost:8080/ws" + authParams

	// In tests or other cases where request is nil
	if r == nil {
		return localDevURL
	}

	log := logger.FromContext(r.Context())

	// Get URL components from the request
	scheme := "ws"
	if r.TLS != nil || strings.HasPrefix(r.Proto, "HTTPS") || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "wss"
	}

	// Get host from request header, strip the port if present
	// This is crucial for production where we don't want ports in WS URLs
	host := r.Host
	if host == "" {
		log.Warn("empty host in request, using fallback mechanisms")

		// Try the internal WebSocket endpoint
		internalURL := "ws://127.0.0.1:8080/ws" + authParams
		log.Info("trying internal WebSocket URL", "url", internalURL)

		// If it fails, return the URL anyway since we need to return something
		return internalURL
	}

	// Remove port from host if present (critical for production)
	if hostParts := strings.Split(host, ":"); len(hostParts) > 1 {
		host = hostParts[0]
	}

	// Determine the base path by removing screen/resolution if present
	basePath := r.URL.Path
	for len(basePath) > 0 && basePath[len(basePath)-1] == '/' {
		basePath = basePath[:len(basePath)-1]
	}

	if len(basePath) >= 18 && basePath[len(basePath)-18:] == "/screen/resolution" {
		basePath = basePath[:len(basePath)-18]
	}

	// Construct WebSocket URL with auth parameters, but NO PORT
	wsURL := fmt.Sprintf("%s://%s%s/ws%s", scheme, host, basePath, authParams)

	// For localhost requests in development, default to the known working port
	if strings.Contains(host, "localhost") {
		// In development, we use a specific port for WebSocket
		wsURL = fmt.Sprintf("ws://localhost:8080/ws%s", authParams)
		log.Info("localhost detected, using development WebSocket URL", "url", wsURL)
		return wsURL
	}

	log.Info("using host-based WebSocket URL", "url", wsURL)
	return wsURL
}

func (s *ApiService) SetScreenResolutionHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	width := 0
	height := 0
	var rate *int

	// Calculate the WebSocket URL from the request
	wsURL := getWebSocketURL(r)

	// Parse width
	widthStr := r.URL.Query().Get("width")
	if widthStr == "" {
		http.Error(w, "missing required query parameter: width", http.StatusBadRequest)
		return
	}
	var err error
	width, err = strconv.Atoi(widthStr)
	if err != nil {
		http.Error(w, "invalid width parameter: must be an integer", http.StatusBadRequest)
		return
	}

	// Parse height
	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		http.Error(w, "missing required query parameter: height", http.StatusBadRequest)
		return
	}
	height, err = strconv.Atoi(heightStr)
	if err != nil {
		http.Error(w, "invalid height parameter: must be an integer", http.StatusBadRequest)
		return
	}

	// Parse optional rate parameter
	rateStr := r.URL.Query().Get("rate")
	if rateStr != "" {
		rateVal, err := strconv.Atoi(rateStr)
		if err != nil {
			http.Error(w, "invalid rate parameter: must be an integer", http.StatusBadRequest)
			return
		}
		rate = &rateVal
	}

	// Create request object
	reqObj := SetScreenResolutionRequestObject{
		Width:  width,
		Height: height,
		Rate:   rate,
		WSURL:  wsURL,
	}

	// Call the actual implementation
	resp, err := s.SetScreenResolution(r.Context(), reqObj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Handle different response types
	switch r := resp.(type) {
	case SetScreenResolution200JSONResponse:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(r)
	case SetScreenResolution400JSONResponse:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(r)
	case SetScreenResolution409JSONResponse:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(r)
	case SetScreenResolution500JSONResponse:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(r)
	default:
		http.Error(w, "unexpected response type", http.StatusInternalServerError)
	}
}

func New(recordManager recorder.RecordManager, factory recorder.FFmpegRecorderFactory) (*ApiService, error) {
	switch {
	case recordManager == nil:
		return nil, fmt.Errorf("recordManager cannot be nil")
	case factory == nil:
		return nil, fmt.Errorf("factory cannot be nil")
	}

	return &ApiService{
		recordManager:     recordManager,
		factory:           factory,
		defaultRecorderID: "default",
		watches:           make(map[string]*fsWatch),
		procs:             make(map[string]*processHandle),
	}, nil
}

func (s *ApiService) StartRecording(ctx context.Context, req oapi.StartRecordingRequestObject) (oapi.StartRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	var params recorder.FFmpegRecordingParams
	if req.Body != nil {
		params.FrameRate = req.Body.Framerate
		params.MaxSizeInMB = req.Body.MaxFileSizeInMB
		params.MaxDurationInSeconds = req.Body.MaxDurationInSeconds
	}

	// Determine recorder ID (use default if none provided)
	recorderID := s.defaultRecorderID
	if req.Body != nil && req.Body.Id != nil && *req.Body.Id != "" {
		recorderID = *req.Body.Id
	}

	// Create, register, and start a new recorder
	rec, err := s.factory(recorderID, params)
	if err != nil {
		log.Error("failed to create recorder", "err", err, "recorder_id", recorderID)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create recording"}}, nil
	}
	if err := s.recordManager.RegisterRecorder(ctx, rec); err != nil {
		if rec, exists := s.recordManager.GetRecorder(recorderID); exists {
			if rec.IsRecording(ctx) {
				log.Error("attempted to start recording while one is already active", "recorder_id", recorderID)
				return oapi.StartRecording409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "recording already in progress"}}, nil
			} else {
				log.Error("attempted to restart recording", "recorder_id", recorderID)
				return oapi.StartRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "recording already completed"}}, nil
			}
		}
		log.Error("failed to register recorder", "err", err, "recorder_id", recorderID)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to register recording"}}, nil
	}

	if err := rec.Start(ctx); err != nil {
		log.Error("failed to start recording", "err", err, "recorder_id", recorderID)
		// ensure the recorder is deregistered
		defer s.recordManager.DeregisterRecorder(ctx, rec)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start recording"}}, nil
	}

	return oapi.StartRecording201Response{}, nil
}

func (s *ApiService) StopRecording(ctx context.Context, req oapi.StopRecordingRequestObject) (oapi.StopRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	// Determine recorder ID
	recorderID := s.defaultRecorderID
	if req.Body != nil && req.Body.Id != nil && *req.Body.Id != "" {
		recorderID = *req.Body.Id
	}

	rec, exists := s.recordManager.GetRecorder(recorderID)
	if !exists {
		log.Error("attempted to stop recording when none is active", "recorder_id", recorderID)
		return oapi.StopRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no active recording to stop"}}, nil
	} else if !rec.IsRecording(ctx) {
		log.Warn("recording already stopped", "recorder_id", recorderID)
		return oapi.StopRecording200Response{}, nil
	}

	// Check if force stop is requested
	forceStop := false
	if req.Body != nil && req.Body.ForceStop != nil {
		forceStop = *req.Body.ForceStop
	}

	var err error
	if forceStop {
		log.Info("force stopping recording", "recorder_id", recorderID)
		err = rec.ForceStop(ctx)
	} else {
		log.Info("gracefully stopping recording", "recorder_id", recorderID)
		err = rec.Stop(ctx)
	}

	if err != nil {
		log.Error("error occurred while stopping recording", "err", err, "force", forceStop, "recorder_id", recorderID)
	}

	return oapi.StopRecording200Response{}, nil
}

const (
	minRecordingSizeInBytes = 100
)

func (s *ApiService) DownloadRecording(ctx context.Context, req oapi.DownloadRecordingRequestObject) (oapi.DownloadRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	// Determine recorder ID
	recorderID := s.defaultRecorderID
	if req.Params.Id != nil && *req.Params.Id != "" {
		recorderID = *req.Params.Id
	}

	// Get the recorder to access its output path
	rec, exists := s.recordManager.GetRecorder(recorderID)
	if !exists {
		log.Error("attempted to download non-existent recording", "recorder_id", recorderID)
		return oapi.DownloadRecording404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "no recording found"}}, nil
	}
	if rec.IsDeleted(ctx) {
		log.Error("attempted to download deleted recording", "recorder_id", recorderID)
		return oapi.DownloadRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "requested recording has been deleted"}}, nil
	}

	out, meta, err := rec.Recording(ctx)
	if err != nil {
		log.Error("failed to get recording", "err", err, "recorder_id", recorderID)
		return oapi.DownloadRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to get recording"}}, nil
	}

	// short-circuit if the recording is still in progress and the file is arbitrary small
	if rec.IsRecording(ctx) && meta.Size <= minRecordingSizeInBytes {
		return oapi.DownloadRecording202Response{
			Headers: oapi.DownloadRecording202ResponseHeaders{
				RetryAfter: 300,
			},
		}, nil
	}

	log.Info("serving recording file for download", "size", meta.Size, "recorder_id", recorderID)
	return oapi.DownloadRecording200Videomp4Response{
		Body: out,
		Headers: oapi.DownloadRecording200ResponseHeaders{
			XRecordingStartedAt:  meta.StartTime.Format(time.RFC3339),
			XRecordingFinishedAt: meta.EndTime.Format(time.RFC3339),
		},
		ContentLength: meta.Size,
	}, nil
}

func (s *ApiService) DeleteRecording(ctx context.Context, req oapi.DeleteRecordingRequestObject) (oapi.DeleteRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	recorderID := s.defaultRecorderID
	if req.Body != nil && req.Body.Id != nil && *req.Body.Id != "" {
		recorderID = *req.Body.Id
	}
	rec, exists := s.recordManager.GetRecorder(recorderID)
	if !exists {
		log.Error("attempted to delete non-existent recording", "recorder_id", recorderID)
		return oapi.DeleteRecording404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "no recording found"}}, nil
	}

	if rec.IsRecording(ctx) {
		log.Error("attempted to delete recording while still in progress", "recorder_id", recorderID)
		return oapi.DeleteRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "recording must be stopped first"}}, nil
	}

	// fine to do this async
	go func() {
		if err := rec.Delete(context.Background()); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Error("failed to delete recording", "err", err, "recorder_id", recorderID)
		} else {
			log.Info("recording deleted", "recorder_id", recorderID)
		}
	}()

	return oapi.DeleteRecording200Response{}, nil
}

// ListRecorders returns a list of all registered recorders and whether each one is currently recording.
func (s *ApiService) ListRecorders(ctx context.Context, _ oapi.ListRecordersRequestObject) (oapi.ListRecordersResponseObject, error) {
	infos := []oapi.RecorderInfo{}

	timeOrNil := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}

	recs := s.recordManager.ListActiveRecorders(ctx)
	for _, r := range recs {
		m := r.Metadata()
		infos = append(infos, oapi.RecorderInfo{
			Id:          r.ID(),
			IsRecording: r.IsRecording(ctx),
			StartedAt:   timeOrNil(m.StartTime),
			FinishedAt:  timeOrNil(m.EndTime),
		})
	}
	return oapi.ListRecorders200JSONResponse(infos), nil
}

func (s *ApiService) Shutdown(ctx context.Context) error {
	return s.recordManager.StopAll(ctx)
}
