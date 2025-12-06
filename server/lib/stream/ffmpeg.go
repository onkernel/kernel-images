package stream

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
	"github.com/onkernel/kernel-images/server/lib/scaletozero"
)

const (
	// exitCodeInitValue mirrors recorder to represent an unset exit code.
	exitCodeInitValue = math.MinInt
)

// FFmpegStreamer streams the display to an RTMP(S) endpoint.
type FFmpegStreamer struct {
	mu sync.Mutex

	id         string
	binaryPath string
	params     Params

	cmd       *exec.Cmd
	exited    chan struct{}
	ffmpegErr error
	exitCode  int
	startedAt time.Time
	stz       *scaletozero.Oncer
}

// NewFFmpegStreamerFactory builds an FFmpeg-backed streamer factory.
func NewFFmpegStreamerFactory(pathToFFmpeg string, defaults Params, ctrl scaletozero.Controller) FFmpegStreamerFactory {
	return func(id string, overrides Params) (Streamer, error) {
		merged := mergeParams(defaults, overrides)
		if err := validateParams(merged); err != nil {
			return nil, err
		}

		return &FFmpegStreamer{
			id:         id,
			binaryPath: pathToFFmpeg,
			params:     merged,
			stz:        scaletozero.NewOncer(ctrl),
			exitCode:   exitCodeInitValue,
		}, nil
	}
}

func mergeParams(defaults Params, overrides Params) Params {
	out := Params{
		FrameRate:         defaults.FrameRate,
		DisplayNum:        defaults.DisplayNum,
		Mode:              defaults.Mode,
		IngestURL:         defaults.IngestURL,
		PlaybackURL:       defaults.PlaybackURL,
		SecurePlaybackURL: defaults.SecurePlaybackURL,
	}
	if overrides.FrameRate != nil {
		out.FrameRate = overrides.FrameRate
	}
	if overrides.DisplayNum != nil {
		out.DisplayNum = overrides.DisplayNum
	}
	if overrides.Mode != "" {
		out.Mode = overrides.Mode
	}
	if overrides.IngestURL != "" {
		out.IngestURL = overrides.IngestURL
	}
	if overrides.PlaybackURL != nil {
		out.PlaybackURL = overrides.PlaybackURL
	}
	if overrides.SecurePlaybackURL != nil {
		out.SecurePlaybackURL = overrides.SecurePlaybackURL
	}
	if out.Mode == "" {
		out.Mode = ModeInternal
	}
	return out
}

func validateParams(p Params) error {
	if p.IngestURL == "" {
		return fmt.Errorf("ingest URL is required")
	}
	if p.FrameRate == nil {
		return fmt.Errorf("frame rate is required")
	}
	if p.DisplayNum == nil {
		return fmt.Errorf("display number is required")
	}
	return nil
}

func (fs *FFmpegStreamer) ID() string {
	return fs.id
}

// Start begins streaming to the configured ingest URL.
func (fs *FFmpegStreamer) Start(ctx context.Context) error {
	log := logger.FromContext(ctx)

	fs.mu.Lock()
	if fs.cmd != nil {
		fs.mu.Unlock()
		return fmt.Errorf("stream already in progress")
	}

	if err := fs.stz.Disable(ctx); err != nil {
		fs.mu.Unlock()
		return fmt.Errorf("failed to disable scale-to-zero: %w", err)
	}

	fs.ffmpegErr = nil
	fs.exitCode = exitCodeInitValue
	fs.startedAt = time.Now()
	fs.exited = make(chan struct{})

	args, err := ffmpegStreamArgs(fs.params)
	if err != nil {
		_ = fs.stz.Enable(context.WithoutCancel(ctx))
		fs.mu.Unlock()
		return err
	}
	log.Info(fmt.Sprintf("%s %s", fs.binaryPath, strings.Join(args, " ")))

	cmd := exec.Command(fs.binaryPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fs.cmd = cmd
	fs.mu.Unlock()

	if err := cmd.Start(); err != nil {
		_ = fs.stz.Enable(context.WithoutCancel(ctx))
		fs.mu.Lock()
		fs.ffmpegErr = err
		fs.cmd = nil
		close(fs.exited)
		fs.mu.Unlock()
		return fmt.Errorf("failed to start ffmpeg process: %w", err)
	}

	go fs.waitForCommand(ctx)

	// Detect immediate startup failures.
	if err := waitForChan(ctx, 250*time.Millisecond, fs.exited); err == nil {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if fs.ffmpegErr != nil {
			return fmt.Errorf("failed to start ffmpeg process: %w", fs.ffmpegErr)
		}
		return fmt.Errorf("ffmpeg process exited immediately with code %d", fs.exitCode)
	}

	return nil
}

// Stop attempts a graceful shutdown of the ffmpeg process, escalating to SIGKILL on timeout.
func (fs *FFmpegStreamer) Stop(ctx context.Context) error {
	defer fs.stz.Enable(context.WithoutCancel(ctx))

	fs.mu.Lock()
	cmd := fs.cmd
	exited := fs.exited
	fs.mu.Unlock()

	if cmd == nil {
		return nil
	}

	// Request graceful stop.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	if err := waitForChan(ctx, 5*time.Second, exited); err == nil {
		return nil
	}

	// Force kill.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = waitForChan(ctx, 2*time.Second, exited)
	return nil
}

func (fs *FFmpegStreamer) IsStreaming(ctx context.Context) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.cmd != nil && fs.cmd.ProcessState == nil
}

func (fs *FFmpegStreamer) Metadata() Metadata {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	return Metadata{
		ID:                fs.id,
		Mode:              fs.params.Mode,
		IngestURL:         fs.params.IngestURL,
		PlaybackURL:       fs.params.PlaybackURL,
		SecurePlaybackURL: fs.params.SecurePlaybackURL,
		StartedAt:         fs.startedAt,
	}
}

func (fs *FFmpegStreamer) waitForCommand(ctx context.Context) {
	defer fs.stz.Enable(context.WithoutCancel(ctx))

	err := fs.cmd.Wait()

	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.ffmpegErr = err
	if fs.cmd.ProcessState != nil {
		fs.exitCode = fs.cmd.ProcessState.ExitCode()
	}
	fs.cmd = nil
	close(fs.exited)

	if err != nil {
		logger.FromContext(ctx).Info("ffmpeg stream exited with error", "err", err, "exitCode", fs.exitCode)
	} else {
		logger.FromContext(ctx).Info("ffmpeg stream exited", "exitCode", fs.exitCode)
	}
}

// ffmpegStreamArgs builds input/output arguments for live streaming.
func ffmpegStreamArgs(params Params) ([]string, error) {
	input, err := screenCaptureArgs(params)
	if err != nil {
		return nil, err
	}

	args := append(input,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(*params.FrameRate*2),
		"-use_wallclock_as_timestamps", "1",
		"-fflags", "nobuffer",
		"-f", "flv",
		params.IngestURL,
	)

	return args, nil
}

func screenCaptureArgs(params Params) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"-f", "avfoundation",
			"-framerate", strconv.Itoa(*params.FrameRate),
			"-pixel_format", "nv12",
			"-i", fmt.Sprintf("%d:none", *params.DisplayNum),
		}, nil
	case "linux":
		return []string{
			"-f", "x11grab",
			"-framerate", strconv.Itoa(*params.FrameRate),
			"-i", fmt.Sprintf(":%d", *params.DisplayNum),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// waitForChan returns nil if and only if the channel is closed within the timeout.
func waitForChan(ctx context.Context, timeout time.Duration, c <-chan struct{}) error {
	select {
	case <-c:
		return nil
	case <-time.After(timeout):
		return errors.New("process did not exit within timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}
