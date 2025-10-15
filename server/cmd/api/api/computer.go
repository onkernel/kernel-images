package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func (s *ApiService) MoveMouse(ctx context.Context, request oapi.MoveMouseRequestObject) (oapi.MoveMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	s.stz.Disable(ctx)
	defer s.stz.Enable(ctx)

	// Validate request body
	if request.Body == nil {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.MoveMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to get current display resolution"}}, nil
	}

	// Ensure non-negative coordinates and within screen bounds
	if body.X < 0 || body.Y < 0 {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
	}
	if body.X >= screenWidth || body.Y >= screenHeight {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)}}, nil
	}

	// Build xdotool arguments
	args := []string{}

	// Hold modifier keys (keydown)
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}

	// Move the cursor to the desired coordinates
	args = append(args, "mousemove", "--sync", strconv.Itoa(body.X), strconv.Itoa(body.Y))

	// Release modifier keys (keyup)
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keyup", key)
		}
	}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		return oapi.MoveMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to move mouse"}}, nil
	}

	return oapi.MoveMouse200Response{}, nil
}

func (s *ApiService) ClickMouse(ctx context.Context, request oapi.ClickMouseRequestObject) (oapi.ClickMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	s.stz.Disable(ctx)
	defer s.stz.Enable(ctx)

	// Validate request body
	if request.Body == nil {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.ClickMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to get current display resolution"}}, nil
	}

	// Ensure non-negative coordinates and within screen bounds
	if body.X < 0 || body.Y < 0 {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
	}
	if body.X >= screenWidth || body.Y >= screenHeight {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)}}, nil
	}

	// Map button enum to xdotool button code. Default to left button.
	btn := "1"
	if body.Button != nil {
		buttonMap := map[oapi.ClickMouseRequestButton]string{
			oapi.Left:    "1",
			oapi.Middle:  "2",
			oapi.Right:   "3",
			oapi.Back:    "8",
			oapi.Forward: "9",
		}
		var ok bool
		btn, ok = buttonMap[*body.Button]
		if !ok {
			return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("unsupported button: %s", *body.Button)}}, nil
		}
	}

	// Determine number of clicks (defaults to 1)
	numClicks := 1
	if body.NumClicks != nil && *body.NumClicks > 0 {
		numClicks = *body.NumClicks
	}

	// Build xdotool arguments
	args := []string{}

	// Hold modifier keys (keydown)
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}

	// Move the cursor
	args = append(args, "mousemove", "--sync", strconv.Itoa(body.X), strconv.Itoa(body.Y))

	// click type defaults to click
	clickType := oapi.Click
	if body.ClickType != nil {
		clickType = *body.ClickType
	}

	// Perform the click action
	switch clickType {
	case oapi.Down:
		args = append(args, "mousedown", btn)
	case oapi.Up:
		args = append(args, "mouseup", btn)
	case oapi.Click:
		args = append(args, "click")
		if numClicks > 1 {
			args = append(args, "--repeat", strconv.Itoa(numClicks))
		}
		args = append(args, btn)
	default:
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("unsupported click type: %s", clickType)}}, nil
	}

	// Release modifier keys (keyup)
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keyup", key)
		}
	}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		return oapi.ClickMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to execute mouse action"}}, nil
	}

	return oapi.ClickMouse200Response{}, nil
}

func (s *ApiService) TakeScreenshot(ctx context.Context, request oapi.TakeScreenshotRequestObject) (oapi.TakeScreenshotResponseObject, error) {
	log := logger.FromContext(ctx)

	s.stz.Disable(ctx)
	defer s.stz.Enable(ctx)

	var body oapi.ScreenshotRequest
	if request.Body != nil {
		body = *request.Body
	}

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to get current display resolution"}}, nil
	}

	// Determine display to use (align with other functions)
	display := s.resolveDisplayFromEnv()

	// Validate region if provided
	if body.Region != nil {
		r := body.Region
		if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 {
			return oapi.TakeScreenshot400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "invalid region dimensions"}}, nil
		}
		if r.X+r.Width > screenWidth || r.Y+r.Height > screenHeight {
			return oapi.TakeScreenshot400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "region exceeds screen bounds"}}, nil
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
	if body.Region != nil {
		r := body.Region
		cropFilter := fmt.Sprintf("crop=%d:%d:%d:%d", r.Width, r.Height, r.X, r.Y)
		args = append(args, "-vf", cropFilter)
	}

	// Output as PNG to stdout
	args = append(args, "-f", "image2pipe", "-vcodec", "png", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", display))

	log.Debug("executing ffmpeg command", "args", args, "display", display)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("failed to create stdout pipe", "err", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("failed to create stderr pipe", "err", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "internal error"}}, nil
	}

	if err := cmd.Start(); err != nil {
		log.Error("failed to start ffmpeg", "err", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to start ffmpeg"}}, nil
	}

	// Start a goroutine to drain stderr for logging to avoid blocking
	go func() {
		data, _ := io.ReadAll(stderr)
		if len(data) > 0 {
			// ffmpeg writes progress/info to stderr; include in debug logs
			enc := base64.StdEncoding.EncodeToString(data)
			log.Debug("ffmpeg stderr (base64)", "data_b64", enc)
		}
	}()

	pr, pw := io.Pipe()
	go func() {
		_, copyErr := io.Copy(pw, stdout)
		waitErr := cmd.Wait()
		var closeErr error
		if copyErr != nil {
			closeErr = fmt.Errorf("streaming ffmpeg output: %w", copyErr)
			log.Error("failed streaming ffmpeg output", "err", copyErr)
		} else if waitErr != nil {
			closeErr = fmt.Errorf("ffmpeg exited with error: %w", waitErr)
			log.Error("ffmpeg exited with error", "err", waitErr)
		}
		if closeErr != nil {
			_ = pw.CloseWithError(closeErr)
			return
		}
		_ = pw.Close()
	}()

	return oapi.TakeScreenshot200ImagepngResponse{Body: pr, ContentLength: 0}, nil
}

func (s *ApiService) TypeText(ctx context.Context, request oapi.TypeTextRequestObject) (oapi.TypeTextResponseObject, error) {
	log := logger.FromContext(ctx)

	s.stz.Disable(ctx)
	defer s.stz.Enable(ctx)

	// Validate request body
	if request.Body == nil {
		return oapi.TypeText400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body

	// Validate delay if provided
	if body.Delay != nil && *body.Delay < 0 {
		return oapi.TypeText400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "delay must be >= 0 milliseconds"}}, nil
	}

	// Build xdotool arguments
	args := []string{"type"}
	if body.Delay != nil {
		args = append(args, "--delay", strconv.Itoa(*body.Delay))
	}
	// Use "--" to terminate options and pass raw text
	args = append(args, "--", body.Text)

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		return oapi.TypeText500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to type text"}}, nil
	}

	return oapi.TypeText200Response{}, nil
}
