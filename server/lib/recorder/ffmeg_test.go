package recorder

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/onkernel/kernel-images/server/lib/scaletozero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	mockBin = filepath.Join("testdata", "mock_ffmpeg.sh")
)

func defaultParams(tempDir string) FFmpegRecordingParams {
	fr := 5
	disp := 0
	size := 1
	return FFmpegRecordingParams{
		FrameRate:   &fr,
		DisplayNum:  &disp,
		MaxSizeInMB: &size,
		OutputDir:   &tempDir,
	}
}

func TestFFmpegRecorder_StartAndStop(t *testing.T) {
	rec := &FFmpegRecorder{
		id:         "startstop",
		binaryPath: mockBin,
		params:     defaultParams(t.TempDir()),
		stz:        scaletozero.NewOncer(scaletozero.NewNoopController()),
	}
	require.NoError(t, rec.Start(t.Context()))
	require.True(t, rec.IsRecording(t.Context()))

	time.Sleep(50 * time.Millisecond)

	err := rec.Stop(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 101")

	<-rec.exited
	require.False(t, rec.IsRecording(t.Context()))
}

func TestFFmpegRecorder_ForceStop(t *testing.T) {
	rec := &FFmpegRecorder{
		id:         "startstop",
		binaryPath: mockBin,
		params:     defaultParams(t.TempDir()),
		stz:        scaletozero.NewOncer(scaletozero.NewNoopController()),
	}
	require.NoError(t, rec.Start(t.Context()))
	require.True(t, rec.IsRecording(t.Context()))

	time.Sleep(50 * time.Millisecond)

	err := rec.ForceStop(t.Context())
	require.Error(t, err)

	<-rec.exited
	require.False(t, rec.IsRecording(t.Context()))
	assert.Contains(t, rec.cmd.ProcessState.String(), "killed")
}
