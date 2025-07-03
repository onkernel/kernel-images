package api

import (
	"bytes"
	"context"
	"io"
	"testing"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
	"github.com/stretchr/testify/require"
)

func TestApiService_StartRecording(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		svc := New(mgr, newMockFactory())

		resp, err := svc.StartRecording(ctx, oapi.StartRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.StartRecording201Response{}, resp)

		rec, exists := mgr.GetRecorder("main")
		require.True(t, exists, "recorder was not registered")
		require.True(t, rec.IsRecording(ctx), "recorder should be recording after Start")
	})

	t.Run("already recording", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		svc := New(mgr, newMockFactory())

		// First start should succeed
		_, err := svc.StartRecording(ctx, oapi.StartRecordingRequestObject{})
		require.NoError(t, err)

		// Second start should return conflict
		resp, err := svc.StartRecording(ctx, oapi.StartRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.StartRecording409JSONResponse{}, resp)
	})
}

func TestApiService_StopRecording(t *testing.T) {
	ctx := context.Background()

	t.Run("no active recording", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		svc := New(mgr, newMockFactory())

		resp, err := svc.StopRecording(ctx, oapi.StopRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.StopRecording400JSONResponse{}, resp)
	})

	t.Run("graceful stop", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		rec := &mockRecorder{id: "main", isRecordingFlag: true}
		require.NoError(t, mgr.RegisterRecorder(ctx, rec), "failed to register recorder")

		svc := New(mgr, newMockFactory())
		resp, err := svc.StopRecording(ctx, oapi.StopRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.StopRecording200Response{}, resp)
		require.True(t, rec.stopCalled, "Stop should have been called on recorder")
	})

	t.Run("force stop", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		rec := &mockRecorder{id: "main", isRecordingFlag: true}
		require.NoError(t, mgr.RegisterRecorder(ctx, rec), "failed to register recorder")

		force := true
		req := oapi.StopRecordingRequestObject{Body: &oapi.StopRecordingJSONRequestBody{ForceStop: &force}}
		svc := New(mgr, newMockFactory())
		resp, err := svc.StopRecording(ctx, req)
		require.NoError(t, err)
		require.IsType(t, oapi.StopRecording200Response{}, resp)
		require.True(t, rec.forceStopCalled, "ForceStop should have been called on recorder")
	})
}

func TestApiService_DownloadRecording(t *testing.T) {
	ctx := context.Background()

	t.Run("not found", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		svc := New(mgr, newMockFactory())
		resp, err := svc.DownloadRecording(ctx, oapi.DownloadRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.DownloadRecording404JSONResponse{}, resp)
	})

	t.Run("still recording", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		rec := &mockRecorder{id: "main", isRecordingFlag: true}
		require.NoError(t, mgr.RegisterRecorder(ctx, rec), "failed to register recorder")

		svc := New(mgr, newMockFactory())
		resp, err := svc.DownloadRecording(ctx, oapi.DownloadRecordingRequestObject{})
		require.NoError(t, err)
		require.IsType(t, oapi.DownloadRecording400JSONResponse{}, resp)
	})

	t.Run("success", func(t *testing.T) {
		mgr := recorder.NewFFmpegManager()
		data := []byte("dummy video data")
		rec := &mockRecorder{id: "main", recordingData: data}
		require.NoError(t, mgr.RegisterRecorder(ctx, rec), "failed to register recorder")

		svc := New(mgr, newMockFactory())
		resp, err := svc.DownloadRecording(ctx, oapi.DownloadRecordingRequestObject{})
		require.NoError(t, err)
		r, ok := resp.(oapi.DownloadRecording200Videomp4Response)
		require.True(t, ok, "expected 200 mp4 response, got %T", resp)
		buf := new(bytes.Buffer)
		_, copyErr := io.Copy(buf, r.Body)
		require.NoError(t, copyErr)
		require.Equal(t, data, buf.Bytes(), "response body mismatch")
		require.Equal(t, int64(len(data)), r.ContentLength, "content length mismatch")
	})
}

func TestApiService_Shutdown(t *testing.T) {
	ctx := context.Background()
	mgr := recorder.NewFFmpegManager()
	rec := &mockRecorder{id: "main", isRecordingFlag: true}
	require.NoError(t, mgr.RegisterRecorder(ctx, rec), "failed to register recorder")

	svc := New(mgr, newMockFactory())

	require.NoError(t, svc.Shutdown(ctx))
	require.True(t, rec.stopCalled, "Shutdown should have stopped active recorder")
}

// mockRecorder is a lightweight stand-in for recorder.Recorder used in unit tests. It purposefully
// keeps the behaviour minimal – just enough to satisfy ApiService logic. All public methods are
// safe for single-goroutine unit-test access.
type mockRecorder struct {
	id              string
	isRecordingFlag bool

	startCalled     bool
	stopCalled      bool
	forceStopCalled bool

	// configurable behaviours
	startErr      error
	stopErr       error
	forceStopErr  error
	recordingErr  error
	recordingData []byte
}

func (m *mockRecorder) ID() string { return m.id }

func (m *mockRecorder) Start(ctx context.Context) error {
	m.startCalled = true
	if m.startErr != nil {
		return m.startErr
	}
	m.isRecordingFlag = true
	return nil
}

func (m *mockRecorder) Stop(ctx context.Context) error {
	m.stopCalled = true
	if m.stopErr != nil {
		return m.stopErr
	}
	m.isRecordingFlag = false
	return nil
}

func (m *mockRecorder) ForceStop(ctx context.Context) error {
	m.forceStopCalled = true
	if m.forceStopErr != nil {
		return m.forceStopErr
	}
	m.isRecordingFlag = false
	return nil
}

func (m *mockRecorder) IsRecording(ctx context.Context) bool { return m.isRecordingFlag }

func (m *mockRecorder) Recording(ctx context.Context) (io.ReadCloser, *recorder.RecordingMetadata, error) {
	if m.recordingErr != nil {
		return nil, nil, m.recordingErr
	}
	reader := io.NopCloser(bytes.NewReader(m.recordingData))
	meta := &recorder.RecordingMetadata{Size: int64(len(m.recordingData))}
	return reader, meta, nil
}

func newMockFactory() recorder.FFmpegRecorderFactory {
	return func(id string, _ recorder.FFmpegRecordingParams) (recorder.Recorder, error) {
		rec := &mockRecorder{id: id}
		return rec, nil
	}
}
