package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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
	// arbitrary value to indicate we have not yet received an exit code from the process
	exitCodeInitValue = math.MinInt

	// the exit codes returned by the stdlib:
	// -1 if the process hasn't exited yet or was terminated by a signal
	// 0 if the process exited successfully
	// >0 if the process exited with a non-zero exit code
	exitCodeProcessDoneMinValue = -1
)

// FFmpegRecorder encapsulates an FFmpeg recording session with platform-specific screen capture.
// It manages the lifecycle of a single FFmpeg process and provides thread-safe operations.
type FFmpegRecorder struct {
	mu sync.Mutex

	id         string
	binaryPath string // path to the ffmpeg binary to execute. Defaults to "ffmpeg".
	cmd        *exec.Cmd
	params     FFmpegRecordingParams
	outputPath string
	startTime  time.Time
	endTime    time.Time
	ffmpegErr  error
	exitCode   int
	exited     chan struct{}
	stz        *scaletozero.Oncer
}

type FFmpegRecordingParams struct {
	FrameRate   *int
	DisplayNum  *int
	MaxSizeInMB *int
	// MaxDurationInSeconds optionally limits the total recording time. If nil there is no duration limit.
	MaxDurationInSeconds *int
	OutputDir            *string
}

func (p FFmpegRecordingParams) Validate() error {
	if p.OutputDir == nil {
		return fmt.Errorf("output directory is required")
	}
	if p.FrameRate == nil {
		return fmt.Errorf("frame rate is required")
	}
	if p.DisplayNum == nil {
		return fmt.Errorf("display number is required")
	}
	if p.MaxSizeInMB == nil {
		return fmt.Errorf("max size in MB is required")
	}
	if p.MaxDurationInSeconds != nil && *p.MaxDurationInSeconds <= 0 {
		return fmt.Errorf("max duration must be greater than 0 seconds")
	}

	return nil
}

type FFmpegRecorderFactory func(id string, overrides FFmpegRecordingParams) (Recorder, error)

// NewFFmpegRecorderFactory returns a factory that creates new recorders. The provided
// pathToFFmpeg is used as the binary to execute; if empty it defaults to "ffmpeg" which
// is expected to be discoverable on the host's PATH.
func NewFFmpegRecorderFactory(pathToFFmpeg string, config FFmpegRecordingParams, ctrl scaletozero.Controller) FFmpegRecorderFactory {
	return func(id string, overrides FFmpegRecordingParams) (Recorder, error) {
		mergedParams := mergeFFmpegRecordingParams(config, overrides)
		return &FFmpegRecorder{
			id:         id,
			binaryPath: pathToFFmpeg,
			outputPath: filepath.Join(*mergedParams.OutputDir, fmt.Sprintf("%s.mp4", id)),
			params:     mergedParams,
			stz:        scaletozero.NewOncer(ctrl),
		}, nil
	}
}

func mergeFFmpegRecordingParams(config FFmpegRecordingParams, overrides FFmpegRecordingParams) FFmpegRecordingParams {
	merged := FFmpegRecordingParams{
		FrameRate:            config.FrameRate,
		DisplayNum:           config.DisplayNum,
		MaxSizeInMB:          config.MaxSizeInMB,
		MaxDurationInSeconds: config.MaxDurationInSeconds,
		OutputDir:            config.OutputDir,
	}
	if overrides.FrameRate != nil {
		merged.FrameRate = overrides.FrameRate
	}
	if overrides.DisplayNum != nil {
		merged.DisplayNum = overrides.DisplayNum
	}
	if overrides.MaxSizeInMB != nil {
		merged.MaxSizeInMB = overrides.MaxSizeInMB
	}
	if overrides.MaxDurationInSeconds != nil {
		merged.MaxDurationInSeconds = overrides.MaxDurationInSeconds
	}
	if overrides.OutputDir != nil {
		merged.OutputDir = overrides.OutputDir
	}

	return merged
}

// ID returns the unique identifier for this recorder.
func (fr *FFmpegRecorder) ID() string {
	return fr.id
}

// Start begins the recording process by launching ffmpeg with the configured parameters.
func (fr *FFmpegRecorder) Start(ctx context.Context) error {
	log := logger.FromContext(ctx)

	fr.mu.Lock()
	if fr.cmd != nil {
		fr.mu.Unlock()
		return fmt.Errorf("recording already in progress")
	}

	if err := fr.stz.Disable(ctx); err != nil {
		fr.mu.Unlock()
		return fmt.Errorf("failed to disable scale-to-zero: %w", err)
	}

	// ensure internal state
	fr.ffmpegErr = nil
	fr.exitCode = exitCodeInitValue
	fr.startTime = time.Now()
	fr.exited = make(chan struct{})

	args, err := ffmpegArgs(fr.params, fr.outputPath)
	if err != nil {
		_ = fr.stz.Enable(ctx)
		fr.cmd = nil
		close(fr.exited)
		fr.mu.Unlock()

		return err
	}
	log.Info(fmt.Sprintf("%s %s", fr.binaryPath, strings.Join(args, " ")))

	cmd := exec.Command(fr.binaryPath, args...)
	// create process group to ensure all processes are signaled together
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	fr.cmd = cmd
	fr.mu.Unlock()

	if err := cmd.Start(); err != nil {
		_ = fr.stz.Enable(ctx)
		fr.mu.Lock()
		fr.ffmpegErr = err
		fr.cmd = nil // reset cmd on failure to start so IsRecording() remains correct
		close(fr.exited)
		fr.mu.Unlock()
		return fmt.Errorf("failed to start ffmpeg process: %w", err)
	}

	// Launch background waiter to capture process completion.
	go fr.waitForCommand(ctx)

	// Check for startup errors before returning
	if err := waitForChan(ctx, 250*time.Millisecond, fr.exited); err == nil {
		fr.mu.Lock()
		defer fr.mu.Unlock()
		return fmt.Errorf("failed to start ffmpeg process: %w", fr.ffmpegErr)
	}

	return nil
}

// Stop gracefully stops the recording using a multi-phase shutdown process.
func (fr *FFmpegRecorder) Stop(ctx context.Context) error {
	defer fr.stz.Enable(ctx)
	err := fr.shutdownInPhases(ctx, []shutdownPhase{
		{"wake_and_interrupt", []syscall.Signal{syscall.SIGCONT, syscall.SIGINT}, 5 * time.Second, "graceful stop"},
		{"retry_interrupt", []syscall.Signal{syscall.SIGINT}, 3 * time.Second, "retry graceful stop"},
		{"terminate", []syscall.Signal{syscall.SIGTERM}, 250 * time.Millisecond, "forceful termination"},
		{"kill", []syscall.Signal{syscall.SIGKILL}, 100 * time.Millisecond, "immediate kill"},
	})

	return err
}

// ForceStop immediately terminates the recording process.
func (fr *FFmpegRecorder) ForceStop(ctx context.Context) error {
	defer fr.stz.Enable(ctx)
	err := fr.shutdownInPhases(ctx, []shutdownPhase{
		{"kill", []syscall.Signal{syscall.SIGKILL}, 100 * time.Millisecond, "immediate kill"},
	})

	return err
}

// IsRecording returns true if a recording is currently in progress.
func (fr *FFmpegRecorder) IsRecording(ctx context.Context) bool {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.cmd != nil && fr.exitCode < exitCodeProcessDoneMinValue
}

// Metadata is an incomplete snapshot of the recording metadata.
func (fr *FFmpegRecorder) Metadata() *RecordingMetadata {
	fr.mu.Lock()
	defer fr.mu.Unlock()

	return &RecordingMetadata{
		StartTime: fr.startTime,
		EndTime:   fr.endTime,
	}
}

// Recording returns the recording file as an io.ReadCloser.
func (fr *FFmpegRecorder) Recording(ctx context.Context) (io.ReadCloser, *RecordingMetadata, error) {
	file, err := os.Open(fr.outputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open recording file: %w", err)
	}

	finfo, err := file.Stat()
	if err != nil {
		// Ensure the file descriptor is not leaked on error
		file.Close()
		return nil, nil, fmt.Errorf("failed to get recording file info: %w", err)
	}

	fr.mu.Lock()
	defer fr.mu.Unlock()
	return file, &RecordingMetadata{
		Size:      finfo.Size(),
		StartTime: fr.startTime,
		EndTime:   fr.endTime,
	}, nil
}

// ffmpegArgs generates platform-specific ffmpeg command line arguments. Allegedly order matters.
func ffmpegArgs(params FFmpegRecordingParams, outputPath string) ([]string, error) {
	var args []string

	// Input options first
	switch runtime.GOOS {
	case "darwin":
		args = []string{
			// Input options for AVFoundation
			"-f", "avfoundation",
			"-framerate", strconv.Itoa(*params.FrameRate),
			"-pixel_format", "nv12",
			// Input file
			"-i", fmt.Sprintf("%d:none", *params.DisplayNum), // Screen capture, no audio
		}
	case "linux":
		args = []string{
			// Input options for X11
			"-f", "x11grab",
			"-framerate", strconv.Itoa(*params.FrameRate),
			// Input file
			"-i", fmt.Sprintf(":%d", *params.DisplayNum), // X11 display
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Output options next
	args = append(args, []string{
		// Video encoding
		"-c:v", "libx264",

		// Timestamp handling for reliable playback
		"-use_wallclock_as_timestamps", "1", // Use system time instead of input stream time
		"-reset_timestamps", "1", // Reset timestamps to start from zero
		"-avoid_negative_ts", "make_zero", // Convert negative timestamps to zero

		// Data safety
		"-movflags", "+frag_keyframe+empty_moov", // Enable fragmented MP4 for data safety
		"-frag_duration", "2000000", // 2-second fragments (in microseconds)
		"-fs", fmt.Sprintf("%dM", *params.MaxSizeInMB), // File size limit
		"-y", // Overwrite output file if it exists
	}...)

	// Duration limit
	if params.MaxDurationInSeconds != nil {
		args = append(args, "-t", strconv.Itoa(*params.MaxDurationInSeconds))
	}

	// Output file
	args = append(args, outputPath)

	return args, nil
}

// waitForCommand should be run in the background to wait for the ffmpeg process to complete and
// update the internal state accordingly.
func (fr *FFmpegRecorder) waitForCommand(ctx context.Context) {
	defer fr.stz.Enable(ctx)

	log := logger.FromContext(ctx)

	// wait for the process to complete and extract the exit code
	err := fr.cmd.Wait()

	// update internal state and cleanup
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.ffmpegErr = err
	fr.exitCode = fr.cmd.ProcessState.ExitCode()
	fr.endTime = time.Now()
	close(fr.exited)

	if err != nil {
		log.Info("ffmpeg process completed with error", "err", err, "exitCode", fr.exitCode)
	} else {
		log.Info("ffmpeg process completed successfully", "exitCode", fr.exitCode)
	}
}

type shutdownPhase struct {
	name    string
	signals []syscall.Signal
	timeout time.Duration
	desc    string
}

func (fr *FFmpegRecorder) shutdownInPhases(ctx context.Context, phases []shutdownPhase) error {
	log := logger.FromContext(ctx)

	// capture immutable references under lock
	fr.mu.Lock()
	exitCode := fr.exitCode
	cmd := fr.cmd
	done := fr.exited
	fr.mu.Unlock()

	if exitCode >= exitCodeProcessDoneMinValue {
		log.Info("ffmpeg process has already exited")
		return nil
	}
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("no recording to stop")
	}

	pgid := -cmd.Process.Pid // negative PGID targets the whole group
	for _, phase := range phases {
		phaseStartTime := time.Now()
		// short circuit: the process exited before this phase started.
		select {
		case <-done:
			return nil
		default:
		}

		log.Info("ffmpeg shutdown phase", "phase", phase.name, "desc", phase.desc)

		// Send the phase's signals in order.
		for idx, sig := range phase.signals {
			_ = syscall.Kill(pgid, sig) // ignore error; process may have gone away
			// arbitrary delay between signals, but not after the last signal
			if idx < len(phase.signals)-1 {
				time.Sleep(100 * time.Millisecond)
			}
		}

		// Wait for exit or timeout
		if err := waitForChan(ctx, phase.timeout-time.Since(phaseStartTime), done); err == nil {
			log.Info("ffmpeg shutdown successful", "phase", phase.name)
			return nil
		}
	}

	return fmt.Errorf("failed to shutdown ffmpeg")
}

// waitForChan returns nil if and only if the channel is closed
func waitForChan(ctx context.Context, timeout time.Duration, c <-chan struct{}) error {
	select {
	case <-c:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("process did not exit within %v timeout", timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type FFmpegManager struct {
	mu        sync.Mutex
	recorders map[string]Recorder
}

func NewFFmpegManager() *FFmpegManager {
	return &FFmpegManager{
		recorders: make(map[string]Recorder),
	}
}

func (fm *FFmpegManager) GetRecorder(id string) (Recorder, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	recorder, exists := fm.recorders[id]
	return recorder, exists
}

func (fm *FFmpegManager) ListActiveRecorders(ctx context.Context) []Recorder {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	recorders := make([]Recorder, 0, len(fm.recorders))
	for _, recorder := range fm.recorders {
		recorders = append(recorders, recorder)
	}

	return recorders
}

func (fm *FFmpegManager) DeregisterRecorder(ctx context.Context, recorder Recorder) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	delete(fm.recorders, recorder.ID())
	return nil
}

func (fm *FFmpegManager) RegisterRecorder(ctx context.Context, recorder Recorder) error {
	log := logger.FromContext(ctx)

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Check for existing recorder with same ID
	if _, exists := fm.recorders[recorder.ID()]; exists {
		return fmt.Errorf("recorder with id '%s' already exists", recorder.ID())
	}

	fm.recorders[recorder.ID()] = recorder
	log.Info("registered new recorder", "id", recorder.ID())
	return nil
}

func (fm *FFmpegManager) StopAll(ctx context.Context) error {
	log := logger.FromContext(ctx)

	fm.mu.Lock()
	defer fm.mu.Unlock()

	var errs []error
	for id, recorder := range fm.recorders {
		if recorder.IsRecording(ctx) {
			if err := recorder.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("failed to stop recorder '%s': %w", id, err))
				log.Error("failed to stop recorder during shutdown", "id", id, "err", err)
			}
		}
	}

	log.Info("stopped all recorders", "count", len(fm.recorders))

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
