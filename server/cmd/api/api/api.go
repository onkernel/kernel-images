package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/onkernel/kernel-images/server/lib/devtoolsproxy"
	"github.com/onkernel/kernel-images/server/lib/logger"
	"github.com/onkernel/kernel-images/server/lib/nekoclient"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
	"github.com/onkernel/kernel-images/server/lib/scaletozero"
	"github.com/onkernel/kernel-images/server/lib/stream"
)

type ApiService struct {
	// defaultRecorderID is used whenever the caller doesn't specify an explicit ID.
	defaultRecorderID string
	defaultStreamID   string

	recordManager  recorder.RecordManager
	factory        recorder.FFmpegRecorderFactory
	streamManager  stream.Manager
	streamFactory  stream.FFmpegStreamerFactory
	rtmpServer     stream.InternalServer
	streamDefaults stream.Params
	// Filesystem watch management
	watchMu sync.RWMutex
	watches map[string]*fsWatch

	// Process management
	procMu sync.RWMutex
	procs  map[string]*processHandle

	// Neko authenticated client
	nekoAuthClient *nekoclient.AuthClient

	// DevTools upstream manager (Chromium supervisord log tailer)
	upstreamMgr *devtoolsproxy.UpstreamManager
	stz         scaletozero.Controller

	// inputMu serializes input-related operations (mouse, keyboard, screenshot)
	inputMu sync.Mutex

	// playwrightMu serializes Playwright code execution (only one execution at a time)
	playwrightMu sync.Mutex
}

var _ oapi.StrictServerInterface = (*ApiService)(nil)

func New(recordManager recorder.RecordManager, factory recorder.FFmpegRecorderFactory, upstreamMgr *devtoolsproxy.UpstreamManager, stz scaletozero.Controller, nekoAuthClient *nekoclient.AuthClient, streamManager stream.Manager, streamFactory stream.FFmpegStreamerFactory, rtmpServer stream.InternalServer, streamDefaults stream.Params) (*ApiService, error) {
	switch {
	case recordManager == nil:
		return nil, fmt.Errorf("recordManager cannot be nil")
	case factory == nil:
		return nil, fmt.Errorf("factory cannot be nil")
	case upstreamMgr == nil:
		return nil, fmt.Errorf("upstreamMgr cannot be nil")
	case nekoAuthClient == nil:
		return nil, fmt.Errorf("nekoAuthClient cannot be nil")
	case streamManager == nil:
		return nil, fmt.Errorf("streamManager cannot be nil")
	case streamFactory == nil:
		return nil, fmt.Errorf("streamFactory cannot be nil")
	case rtmpServer == nil:
		return nil, fmt.Errorf("rtmpServer cannot be nil")
	}
	if streamDefaults.FrameRate == nil || streamDefaults.DisplayNum == nil {
		return nil, fmt.Errorf("streamDefaults must include frame rate and display number")
	}

	return &ApiService{
		recordManager:     recordManager,
		factory:           factory,
		defaultRecorderID: "default",
		streamManager:     streamManager,
		streamFactory:     streamFactory,
		rtmpServer:        rtmpServer,
		streamDefaults:    streamDefaults,
		defaultStreamID:   "default",
		watches:           make(map[string]*fsWatch),
		procs:             make(map[string]*processHandle),
		upstreamMgr:       upstreamMgr,
		stz:               stz,
		nekoAuthClient:    nekoAuthClient,
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

func (s *ApiService) StartStream(ctx context.Context, req oapi.StartStreamRequestObject) (oapi.StartStreamResponseObject, error) {
	log := logger.FromContext(ctx)

	if req.Body == nil {
		return oapi.StartStream400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}

	streamID := s.defaultStreamID
	if req.Body.Id != nil && *req.Body.Id != "" {
		streamID = *req.Body.Id
	}

	mode := stream.ModeInternal
	if req.Body.Mode != nil && *req.Body.Mode != "" {
		mode = stream.Mode(*req.Body.Mode)
	}
	if mode != stream.ModeInternal && mode != stream.ModeRemote {
		return oapi.StartStream400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid stream mode"}}, nil
	}

	frameRate := s.streamDefaults.FrameRate
	if req.Body.Framerate != nil {
		frameRate = req.Body.Framerate
	}

	if existing, ok := s.streamManager.GetStream(streamID); ok {
		if existing.IsStreaming(ctx) {
			return oapi.StartStream409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "stream already in progress"}}, nil
		}
		_ = s.streamManager.DeregisterStream(ctx, existing)
	}

	var ingestURL string
	var playbackURL *string
	var securePlaybackURL *string
	streamPath := fmt.Sprintf("live/%s", streamID)

	switch mode {
	case stream.ModeInternal:
		if err := s.rtmpServer.Start(ctx); err != nil {
			log.Error("failed to start internal rtmp server", "err", err)
			return oapi.StartStream500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start internal streaming server"}}, nil
		}
		s.rtmpServer.EnsureStream(streamPath)
		ingestURL = s.rtmpServer.IngestURL(streamPath)
		playbackURL, securePlaybackURL = s.rtmpServer.PlaybackURLs("", streamPath)
	case stream.ModeRemote:
		if req.Body.TargetUrl == nil || *req.Body.TargetUrl == "" {
			return oapi.StartStream400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "target_url is required for remote streaming"}}, nil
		}
		ingestURL = *req.Body.TargetUrl
		playbackURL = &ingestURL
	}

	params := stream.Params{
		FrameRate:         frameRate,
		DisplayNum:        s.streamDefaults.DisplayNum,
		Mode:              mode,
		IngestURL:         ingestURL,
		PlaybackURL:       playbackURL,
		SecurePlaybackURL: securePlaybackURL,
	}

	streamer, err := s.streamFactory(streamID, params)
	if err != nil {
		log.Error("failed to create streamer", "err", err, "stream_id", streamID)
		return oapi.StartStream500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create streamer"}}, nil
	}
	if err := s.streamManager.RegisterStream(ctx, streamer); err != nil {
		log.Error("failed to register stream", "err", err, "stream_id", streamID)
		return oapi.StartStream409JSONResponse{ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{Message: "stream already exists"}}, nil
	}
	if err := streamer.Start(ctx); err != nil {
		log.Error("failed to start stream", "err", err, "stream_id", streamID)
		_ = s.streamManager.DeregisterStream(ctx, streamer)
		return oapi.StartStream500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start stream"}}, nil
	}

	return oapi.StartStream201JSONResponse(streamMetadataToOAPI(streamer.Metadata(), streamer.IsStreaming(ctx))), nil
}

func (s *ApiService) StopStream(ctx context.Context, req oapi.StopStreamRequestObject) (oapi.StopStreamResponseObject, error) {
	log := logger.FromContext(ctx)

	streamID := s.defaultStreamID
	if req.Body != nil && req.Body.Id != nil && *req.Body.Id != "" {
		streamID = *req.Body.Id
	}

	streamer, ok := s.streamManager.GetStream(streamID)
	if !ok {
		return oapi.StopStream404JSONResponse{NotFoundErrorJSONResponse: oapi.NotFoundErrorJSONResponse{Message: "stream not found"}}, nil
	}

	if err := streamer.Stop(ctx); err != nil {
		log.Error("failed to stop stream", "err", err, "stream_id", streamID)
	}
	_ = s.streamManager.DeregisterStream(ctx, streamer)

	return oapi.StopStream200Response{}, nil
}

func (s *ApiService) ListStreams(ctx context.Context, _ oapi.ListStreamsRequestObject) (oapi.ListStreamsResponseObject, error) {
	streams := s.streamManager.ListStreams(ctx)
	infos := make([]oapi.StreamInfo, 0, len(streams))
	for _, st := range streams {
		infos = append(infos, streamMetadataToOAPI(st.Metadata(), st.IsStreaming(ctx)))
	}
	return oapi.ListStreams200JSONResponse(infos), nil
}

func streamMetadataToOAPI(meta stream.Metadata, isStreaming bool) oapi.StreamInfo {
	return oapi.StreamInfo{
		Id:                meta.ID,
		Mode:              oapi.StreamInfoMode(meta.Mode),
		IngestUrl:         meta.IngestURL,
		PlaybackUrl:       meta.PlaybackURL,
		SecurePlaybackUrl: meta.SecurePlaybackURL,
		StartedAt:         meta.StartedAt,
		IsStreaming:       isStreaming,
	}
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
	var errs []error
	if err := s.recordManager.StopAll(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.streamManager.StopAll(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.rtmpServer.Close(ctx); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
