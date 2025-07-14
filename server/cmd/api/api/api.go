package api

import (
	"context"
	"fmt"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
)

type ApiService struct {
	// defaultRecorderID is used whenever the caller doesn't specify an explicit ID.
	defaultRecorderID string

	recordManager recorder.RecordManager
	factory       recorder.FFmpegRecorderFactory
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
		defaultRecorderID: "main",
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
		log.Error("failed to create recorder", "err", err)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create recording"}}, nil
	}
	if err := s.recordManager.RegisterRecorder(ctx, rec); err != nil {
		if rec, exists := s.recordManager.GetRecorder(recorderID); exists && rec.IsRecording(ctx) {
			log.Error("attempted to start recording while one is already active")
			return oapi.StartRecording409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "recording already in progress"}}, nil
		}
		log.Error("failed to register recorder", "err", err)
		return oapi.StartRecording500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to register recording"}}, nil
	}

	if err := rec.Start(ctx); err != nil {
		log.Error("failed to start recording", "err", err)
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
		log.Warn("attempted to stop recording when none is active")
		return oapi.StopRecording400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "no active recording to stop"}}, nil
	} else if !rec.IsRecording(ctx) {
		log.Warn("recording already stopped")
		return oapi.StopRecording200Response{}, nil
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
		log.Error("attempted to download non-existent recording")
		return oapi.DownloadRecording404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "no recording found"}}, nil
	}

	out, meta, err := rec.Recording(ctx)
	if err != nil {
		log.Error("failed to get recording", "err", err)
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

	log.Info("serving recording file for download", "size", meta.Size)
	return oapi.DownloadRecording200Videomp4Response{
		Body: out,
		Headers: oapi.DownloadRecording200ResponseHeaders{
			XRecordingStart: meta.StartTime,
			XRecordingEnd:   meta.EndTime,
		},
		ContentLength: meta.Size,
	}, nil
}

func (s *ApiService) Shutdown(ctx context.Context) error {
	return s.recordManager.StopAll(ctx)
}
