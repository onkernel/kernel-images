package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// getScreenDimensions uses xdpyinfo to get the screen dimensions
func getScreenDimensions(display string) (width, height int, err error) {
	cmd := exec.Command("xdpyinfo")
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", display))

	output, err := cmd.Output()
	if err != nil {
		// Fallback to default dimensions if xdpyinfo fails
		return 1024, 768, nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "dimensions:") {
			// Parse line like "  dimensions:    1920x1080 pixels (508x285 millimeters)"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				dims := strings.Split(parts[1], "x")
				if len(dims) == 2 {
					w, _ := strconv.Atoi(dims[0])
					h, _ := strconv.Atoi(dims[1])
					if w > 0 && h > 0 {
						return w, h, nil
					}
				}
			}
		}
	}

	// Default dimensions if parsing fails
	return 1024, 768, nil
}

// captureScreenshot captures a screenshot using ffmpeg
func captureScreenshot(ctx context.Context, display string, region *oapi.ScreenshotRegionRequest) (io.Reader, error) {
	log := logger.FromContext(ctx)

	// Get screen dimensions for bounds checking
	screenWidth, screenHeight, err := getScreenDimensions(display)
	if err != nil {
		log.Warn("failed to get screen dimensions, using defaults", "error", err)
	}

	// Validate region bounds if provided
	if region != nil {
		if region.X < 0 || region.Y < 0 {
			return nil, fmt.Errorf("coordinates must be non-negative")
		}
		if region.Width <= 0 || region.Height <= 0 {
			return nil, fmt.Errorf("width and height must be positive")
		}
		if region.X+region.Width > screenWidth || region.Y+region.Height > screenHeight {
			return nil, fmt.Errorf("region exceeds screen bounds (screen: %dx%d, region: %d,%d %dx%d)",
				screenWidth, screenHeight, region.X, region.Y, region.Width, region.Height)
		}
	}

	// Build ffmpeg command
	args := []string{
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", screenWidth, screenHeight),
		"-i", display,
		"-vframes", "1",
	}

	// Add crop filter if region is specified
	if region != nil {
		cropFilter := fmt.Sprintf("crop=%d:%d:%d:%d", region.Width, region.Height, region.X, region.Y)
		args = append(args, "-vf", cropFilter)
	}

	// Output as PNG to stdout
	args = append(args, "-f", "image2pipe", "-vcodec", "png", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", display))

	log.Debug("executing ffmpeg command", "args", args, "display", display)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Error("ffmpeg failed", "stderr", string(exitErr.Stderr), "error", err)
			return nil, fmt.Errorf("screenshot capture failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("screenshot capture failed: %w", err)
	}

	return bytes.NewReader(output), nil
}

// CaptureScreenshot implements the GET /screenshot endpoint
func (s *ApiService) CaptureScreenshot(ctx context.Context, _ oapi.CaptureScreenshotRequestObject) (oapi.CaptureScreenshotResponseObject, error) {
	log := logger.FromContext(ctx)

	// Get display from environment, default to :1 for production
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":1"
	}

	log.Info("capturing full screenshot", "display", display)

	screenshot, err := captureScreenshot(ctx, display, nil)
	if err != nil {
		log.Error("failed to capture screenshot", "error", err)
		return oapi.CaptureScreenshot500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: err.Error(),
			},
		}, nil
	}

	// Read the screenshot data
	data, err := io.ReadAll(screenshot)
	if err != nil {
		log.Error("failed to read screenshot data", "error", err)
		return oapi.CaptureScreenshot500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: "failed to read screenshot data",
			},
		}, nil
	}

	return oapi.CaptureScreenshot200ImagepngResponse{
		Body:          bytes.NewReader(data),
		ContentLength: int64(len(data)),
	}, nil
}

// CaptureScreenshotRegion implements the POST /screenshot endpoint
func (s *ApiService) CaptureScreenshotRegion(ctx context.Context, req oapi.CaptureScreenshotRegionRequestObject) (oapi.CaptureScreenshotRegionResponseObject, error) {
	log := logger.FromContext(ctx)

	// Get display from environment, default to :1 for production
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":1"
	}

	log.Info("capturing screenshot region", "display", display,
		"x", req.Body.X, "y", req.Body.Y,
		"width", req.Body.Width, "height", req.Body.Height)

	screenshot, err := captureScreenshot(ctx, display, req.Body)
	if err != nil {
		log.Error("failed to capture screenshot region", "error", err)
		// Check if it's a bounds error
		if strings.Contains(err.Error(), "exceeds screen bounds") ||
			strings.Contains(err.Error(), "must be") {
			return oapi.CaptureScreenshotRegion400JSONResponse{
				BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
					Message: err.Error(),
				},
			}, nil
		}
		return oapi.CaptureScreenshotRegion500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: err.Error(),
			},
		}, nil
	}

	// Read the screenshot data
	data, err := io.ReadAll(screenshot)
	if err != nil {
		log.Error("failed to read screenshot data", "error", err)
		return oapi.CaptureScreenshotRegion500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: "failed to read screenshot data",
			},
		}, nil
	}

	return oapi.CaptureScreenshotRegion200ImagepngResponse{
		Body:          bytes.NewReader(data),
		ContentLength: int64(len(data)),
	}, nil
}
