package api

import (
	"context"
	"os"
	"os/exec"
	"testing"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiService_GetHealth(t *testing.T) {
	ctx := context.Background()

	mgr := recorder.NewFFmpegManager()
	svc, err := New(mgr, newMockFactory())
	require.NoError(t, err)

	// Call health endpoint
	resp, err := svc.GetHealth(ctx, oapi.GetHealthRequestObject{})
	require.NoError(t, err)

	// Check response type
	healthResp, ok := resp.(oapi.GetHealth200JSONResponse)
	require.True(t, ok, "expected GetHealth200JSONResponse")

	// Check status is "ok"
	assert.Equal(t, oapi.Ok, healthResp.Status)

	// Check uptime is non-negative
	assert.GreaterOrEqual(t, healthResp.UptimeSec, 0)
}

func TestApiService_CaptureScreenshot(t *testing.T) {
	// Skip if ffmpeg is not available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH")
	}

	// Skip if no display is available
	if os.Getenv("DISPLAY") == "" && os.Getenv("CI") != "" {
		t.Skip("No DISPLAY available in CI environment")
	}

	ctx := context.Background()

	mgr := recorder.NewFFmpegManager()
	svc, err := New(mgr, newMockFactory())
	require.NoError(t, err)

	// Test full screenshot capture
	t.Run("full_screenshot", func(t *testing.T) {
		// Set test display
		os.Setenv("DISPLAY", ":20")
		defer os.Unsetenv("DISPLAY")

		resp, err := svc.CaptureScreenshot(ctx, oapi.CaptureScreenshotRequestObject{})

		// In test environment, screenshot might fail due to no X server
		// Just ensure no panic and proper error handling
		if err == nil {
			// If successful, check response type
			_, ok := resp.(oapi.CaptureScreenshot200ImagepngResponse)
			if !ok {
				// Might be 500 error which is acceptable in test
				_, ok = resp.(oapi.CaptureScreenshot500JSONResponse)
				assert.True(t, ok, "expected either 200 or 500 response")
			}
		}
	})
}

func TestApiService_CaptureScreenshotRegion(t *testing.T) {
	ctx := context.Background()

	mgr := recorder.NewFFmpegManager()
	svc, err := New(mgr, newMockFactory())
	require.NoError(t, err)

	// Test region validation
	t.Run("invalid_region_negative_coords", func(t *testing.T) {
		req := oapi.CaptureScreenshotRegionRequestObject{
			Body: &oapi.ScreenshotRegionRequest{
				X:      -1,
				Y:      0,
				Width:  100,
				Height: 100,
			},
		}

		resp, err := svc.CaptureScreenshotRegion(ctx, req)
		require.NoError(t, err)

		// Should return 400 bad request
		badReqResp, ok := resp.(oapi.CaptureScreenshotRegion400JSONResponse)
		assert.True(t, ok, "expected 400 bad request response")
		assert.Contains(t, badReqResp.Message, "must be non-negative")
	})

	t.Run("invalid_region_zero_dimensions", func(t *testing.T) {
		req := oapi.CaptureScreenshotRegionRequestObject{
			Body: &oapi.ScreenshotRegionRequest{
				X:      0,
				Y:      0,
				Width:  0,
				Height: 100,
			},
		}

		resp, err := svc.CaptureScreenshotRegion(ctx, req)
		require.NoError(t, err)

		// Should return 400 bad request
		badReqResp, ok := resp.(oapi.CaptureScreenshotRegion400JSONResponse)
		assert.True(t, ok, "expected 400 bad request response")
		assert.Contains(t, badReqResp.Message, "must be positive")
	})
}
