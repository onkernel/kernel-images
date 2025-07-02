package api

import (
	"context"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
)

// ApiService implements the API endpoints
// It manages a single recording session and provides endpoints for starting, stopping, and downloading it
type ApiService struct {
	mainRecorderID string // ID used for the primary recording session
	recordManager  recorder.RecordManager
	factory        recorder.FFmpegRecorderFactory
}

func New(recordManager recorder.RecordManager, factory recorder.FFmpegRecorderFactory) *ApiService {
	return &ApiService{
		recordManager:  recordManager,
		factory:        factory,
		mainRecorderID: "main", // use a single recorder for now
	}
}

func (s *ApiService) StartRecording(ctx context.Context, req oapi.StartRecordingRequestObject) (oapi.StartRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	if rec, exists := s.recordManager.GetRecorder(s.mainRecorderID); exists && rec.IsRecording(ctx) {
		log.Error("attempted to start recording while one is already active")
		return oapi.StartRecording409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "recording already in progress"}}, nil
	}

	var params recorder.FFmpegRecordingParams
	if req.Body != nil {
		params.FrameRate = req.Body.Framerate
		params.MaxSizeInMB = req.Body.MaxFileSizeInMB
	}

	// Create, register, and start a new recorder
	rec, err := s.factory(s.mainRecorderID, params)
	if err != nil {
		log.Error("failed to create recorder", "err", err)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create recording"}}, nil
	}
	if err := s.recordManager.RegisterRecorder(ctx, rec); err != nil {
		log.Error("failed to register recorder", "err", err)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to register recording"}}, nil
	}

	if err := rec.Start(ctx); err != nil {
		log.Error("failed to start recording", "err", err)
		// ensure the recorder is deregistered if we fail to start
		defer s.recordManager.DeregisterRecorder(ctx, rec)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start recording"}}, nil
	}

	return oapi.StartRecording201Response{}, nil
}

func (s *ApiService) StopRecording(ctx context.Context, req oapi.StopRecordingRequestObject) (oapi.StopRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	rec, exists := s.recordManager.GetRecorder(s.mainRecorderID)
	if !exists || !rec.IsRecording(ctx) {
		log.Warn("attempted to stop recording when none is active")
		return oapi.StopRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no active recording to stop"}}, nil
	}

	// Check if force stop is requested
	forceStop := false
	if req.Body != nil && req.Body.ForceStop != nil {
		forceStop = *req.Body.ForceStop
	}

	var err error
	if forceStop {
		log.Info("force stopping recording")
		err = rec.ForceStop(ctx)
	} else {
		log.Info("gracefully stopping recording")
		err = rec.Stop(ctx)
	}

	if err != nil {
		log.Error("error occurred while stopping recording", "err", err, "force", forceStop)
	}

	return oapi.StopRecording200Response{}, nil
}

func (s *ApiService) DownloadRecording(ctx context.Context, req oapi.DownloadRecordingRequestObject) (oapi.DownloadRecordingResponseObject, error) {
	log := logger.FromContext(ctx)

	// Get the recorder to access its output path
	rec, exists := s.recordManager.GetRecorder(s.mainRecorderID)
	if !exists {
		log.Error("attempted to download non-existent recording")
		return oapi.DownloadRecording404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "no recording found"}}, nil
	}

	if rec.IsRecording(ctx) {
		log.Warn("attempted to download recording while is still in progress")
		return oapi.DownloadRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "recording still in progress, please stop first"}}, nil
	}

	out, meta, err := rec.Recording(ctx)
	if err != nil {
		log.Error("failed to get recording", "err", err)
		return oapi.DownloadRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to get recording"}}, nil
	}

	log.Info("serving recording file for download", "size", meta.Size)
	return oapi.DownloadRecording200Videomp4Response{
		Body:          out,
		ContentLength: meta.Size,
	}, nil
}

func (s *ApiService) Shutdown(ctx context.Context) error {
	return s.recordManager.StopAll(ctx)
}
