package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: false,
		},
		{
			name: "display num too high",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   100,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "DISPLAY_NUM must be between 0 and 99",
		},
		{
			name: "display num negative",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   -1,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "DISPLAY_NUM must be between 0 and 99",
		},
		{
			name: "frame rate zero",
			config: &Config{
				Port:         8080,
				FrameRate:    0,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "FRAME_RATE must be between 1 and 120",
		},
		{
			name: "frame rate too high",
			config: &Config{
				Port:         8080,
				FrameRate:    121,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "FRAME_RATE must be between 1 and 120",
		},
		{
			name: "max size zero",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   1,
				MaxSizeInMB:  0,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "MAX_SIZE_MB must be between 1 and 10000",
		},
		{
			name: "max size too high",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   1,
				MaxSizeInMB:  10001,
				OutputDir:    "/tmp",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "MAX_SIZE_MB must be between 1 and 10000",
		},
		{
			name: "empty output dir",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    "",
				PathToFFmpeg: "ffmpeg",
			},
			expectError: true,
			errorMsg:    "OUTPUT_DIR is required",
		},
		{
			name: "empty ffmpeg path",
			config: &Config{
				Port:         8080,
				FrameRate:    30,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    "/tmp",
				PathToFFmpeg: "",
			},
			expectError: true,
			errorMsg:    "FFMPEG_PATH is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.config)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLogSafeConfig(t *testing.T) {
	config := &Config{
		Port:         8080,
		FrameRate:    30,
		DisplayNum:   1,
		MaxSizeInMB:  500,
		OutputDir:    "/sensitive/path",
		PathToFFmpeg: "/usr/local/bin/ffmpeg",
	}

	logSafe := config.LogSafeConfig()

	// Check that non-sensitive fields are preserved
	assert.Equal(t, 8080, logSafe["port"])
	assert.Equal(t, 30, logSafe["frame_rate"])
	assert.Equal(t, 1, logSafe["display_num"])
	assert.Equal(t, 500, logSafe["max_size_mb"])

	// Check that sensitive fields are redacted
	assert.Equal(t, "[REDACTED]", logSafe["output_dir"])
	assert.Equal(t, "[REDACTED]", logSafe["ffmpeg_path"])

	// Ensure original config is not modified
	assert.Equal(t, "/sensitive/path", config.OutputDir)
	assert.Equal(t, "/usr/local/bin/ffmpeg", config.PathToFFmpeg)
}