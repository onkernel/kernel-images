package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration for the server
type Config struct {
	// Server configuration
	Port int `envconfig:"PORT" default:"10001"`

	// Recording configuration
	FrameRate   int    `envconfig:"FRAME_RATE" default:"10"`
	DisplayNum  int    `envconfig:"DISPLAY_NUM" default:"1"`
	MaxSizeInMB int    `envconfig:"MAX_SIZE_MB" default:"500"`
	OutputDir   string `envconfig:"OUTPUT_DIR" default:"."`

	// Absolute or relative path to the ffmpeg binary. If empty the code falls back to "ffmpeg" on $PATH.
	PathToFFmpeg string `envconfig:"FFMPEG_PATH" default:"ffmpeg"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return nil, err
	}
	if err := validate(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func validate(config *Config) error {
	if config.OutputDir == "" {
		return fmt.Errorf("OUTPUT_DIR is required")
	}
	if config.DisplayNum < 0 {
		return fmt.Errorf("DISPLAY_NUM must be greater than 0")
	}
	if config.FrameRate < 0 {
		return fmt.Errorf("FRAME_RATE must be greater than 0")
	}
	if config.MaxSizeInMB < 0 {
		return fmt.Errorf("MAX_SIZE_MB must be greater than 0")
	}
	if config.PathToFFmpeg == "" {
		return fmt.Errorf("FFMPEG_PATH is required")
	}

	return nil
}
