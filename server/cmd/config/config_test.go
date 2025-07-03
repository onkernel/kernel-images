package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	testCases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		wantCfg *Config
	}{
		{
			name: "defaults (no env set)",
			env:  map[string]string{},
			wantCfg: &Config{
				Port:         10001,
				FrameRate:    10,
				DisplayNum:   1,
				MaxSizeInMB:  500,
				OutputDir:    ".",
				PathToFFmpeg: "ffmpeg",
			},
		},
		{
			name: "custom valid env",
			env: map[string]string{
				"PORT":        "12345",
				"FRAME_RATE":  "24",
				"DISPLAY_NUM": "2",
				"MAX_SIZE_MB": "250",
				"OUTPUT_DIR":  "/tmp",
				"FFMPEG_PATH": "/usr/local/bin/ffmpeg",
			},
			wantCfg: &Config{
				Port:         12345,
				FrameRate:    24,
				DisplayNum:   2,
				MaxSizeInMB:  250,
				OutputDir:    "/tmp",
				PathToFFmpeg: "/usr/local/bin/ffmpeg",
			},
		},
		{
			name: "negative display num",
			env: map[string]string{
				"DISPLAY_NUM": "-1",
			},
			wantErr: true,
		},
		{
			name: "frame rate too high",
			env: map[string]string{
				"FRAME_RATE": "1201",
			},
			wantErr: true,
		},
		{
			name: "max size too big",
			env: map[string]string{
				"MAX_SIZE_MB": "10001",
			},
			wantErr: true,
		},
		{
			name: "missing ffmpeg path (set to empty)",
			env: map[string]string{
				"FFMPEG_PATH": "",
			},
			wantErr: true,
		},
		{
			name: "missing output dir (set to empty)",
			env: map[string]string{
				"OUTPUT_DIR": "",
			},
			wantErr: true,
		},
	}

	for idx := range testCases {
		tc := testCases[idx]
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := Load()

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
				require.Equal(t, tc.wantCfg, cfg)
			}
		})
	}
}
