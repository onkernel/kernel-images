package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func (s *ApiService) MoveMouse(ctx context.Context, request oapi.MoveMouseRequestObject) (oapi.MoveMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	// Validate request body
	if request.Body == nil {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"},
		}, nil
	}
	body := *request.Body

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.MoveMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to get current display resolution"},
		}, nil
	}

	// Ensure non-negative coordinates and within screen bounds
	if body.X < 0 || body.Y < 0 {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "coordinates must be non-negative"},
		}, nil
	}
	if body.X >= screenWidth || body.Y >= screenHeight {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)},
		}, nil
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
		return oapi.MoveMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to move mouse"},
		}, nil
	}

	return oapi.MoveMouse200Response{}, nil
}

func (s *ApiService) ClickMouse(ctx context.Context, request oapi.ClickMouseRequestObject) (oapi.ClickMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	// Validate request body
	if request.Body == nil {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"},
		}, nil
	}
	body := *request.Body

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.ClickMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to get current display resolution"},
		}, nil
	}

	// Ensure non-negative coordinates and within screen bounds
	if body.X < 0 || body.Y < 0 {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "coordinates must be non-negative"},
		}, nil
	}
	if body.X >= screenWidth || body.Y >= screenHeight {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)},
		}, nil
	}

	// Map button enum to xdotool button code. Default to left button.
	btn := "1"
	if body.Button != nil {
		buttonMap := map[oapi.ClickMouseRequestButton]string{
			oapi.ClickMouseRequestButtonLeft:    "1",
			oapi.ClickMouseRequestButtonMiddle:  "2",
			oapi.ClickMouseRequestButtonRight:   "3",
			oapi.ClickMouseRequestButtonBack:    "8",
			oapi.ClickMouseRequestButtonForward: "9",
		}
		var ok bool
		btn, ok = buttonMap[*body.Button]
		if !ok {
			return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: fmt.Sprintf("unsupported button: %s", *body.Button)},
			}, nil
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
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: fmt.Sprintf("unsupported click type: %s", clickType)},
		}, nil
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
		return oapi.ClickMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to execute mouse action"},
		}, nil
	}

	return oapi.ClickMouse200Response{}, nil
}

func (s *ApiService) TakeScreenshot(ctx context.Context, request oapi.TakeScreenshotRequestObject) (oapi.TakeScreenshotResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid race with other input/screen actions
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	var body oapi.ScreenshotRequest
	if request.Body != nil {
		body = *request.Body
	}

	// Get current resolution for bounds validation
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to get current display resolution"},
		}, nil
	}

	// Determine display to use (align with other functions)
	display := s.resolveDisplayFromEnv()

	// Validate region if provided
	if body.Region != nil {
		r := body.Region
		if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 {
			return oapi.TakeScreenshot400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "invalid region dimensions"},
			}, nil
		}
		if r.X+r.Width > screenWidth || r.Y+r.Height > screenHeight {
			return oapi.TakeScreenshot400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "region exceeds screen bounds"},
			}, nil
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
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "internal error"},
		}, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("failed to create stderr pipe", "err", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "internal error"},
		}, nil
	}

	if err := cmd.Start(); err != nil {
		log.Error("failed to start ffmpeg", "err", err)
		return oapi.TakeScreenshot500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to start ffmpeg"},
		}, nil
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

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	// Validate request body
	if request.Body == nil {
		return oapi.TypeText400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"},
		}, nil
	}
	body := *request.Body

	// Validate delay if provided
	if body.Delay != nil && *body.Delay < 0 {
		return oapi.TypeText400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "delay must be >= 0 milliseconds"},
		}, nil
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
		return oapi.TypeText500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to type text"},
		}, nil
	}

	return oapi.TypeText200Response{}, nil
}

func (s *ApiService) PressKey(ctx context.Context, request oapi.PressKeyRequestObject) (oapi.PressKeyResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	if request.Body == nil {
		return oapi.PressKey400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"},
		}, nil
	}
	body := *request.Body

	if len(body.Keys) == 0 {
		return oapi.PressKey400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "keys must contain at least one key symbol"},
		}, nil
	}
	if body.Duration != nil && *body.Duration < 0 {
		return oapi.PressKey400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "duration must be >= 0 milliseconds"},
		}, nil
	}

	// If duration is provided and >0, hold all keys down, sleep, then release.
	if body.Duration != nil && *body.Duration > 0 {
		argsDown := []string{}
		if body.HoldKeys != nil {
			for _, key := range *body.HoldKeys {
				argsDown = append(argsDown, "keydown", key)
			}
		}
		for _, key := range body.Keys {
			argsDown = append(argsDown, "keydown", key)
		}

		log.Info("executing xdotool (keydown phase)", "args", argsDown)
		if output, err := defaultXdoTool.Run(ctx, argsDown...); err != nil {
			log.Error("xdotool keydown failed", "err", err, "output", string(output))
			// Best-effort release any keys that may be down (primary and modifiers)
			argsUp := []string{}
			for _, key := range body.Keys {
				argsUp = append(argsUp, "keyup", key)
			}
			if body.HoldKeys != nil {
				for _, key := range *body.HoldKeys {
					argsUp = append(argsUp, "keyup", key)
				}
			}
			_, _ = defaultXdoTool.Run(ctx, argsUp...)
			return oapi.PressKey500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to press keys (keydown). out=%s", string(output))},
			}, nil
		}

		d := time.Duration(*body.Duration) * time.Millisecond
		time.Sleep(d)

		argsUp := []string{}
		for _, key := range body.Keys {
			argsUp = append(argsUp, "keyup", key)
		}
		if body.HoldKeys != nil {
			for _, key := range *body.HoldKeys {
				argsUp = append(argsUp, "keyup", key)
			}
		}

		log.Info("executing xdotool (keyup phase)", "args", argsUp)
		if output, err := defaultXdoTool.Run(ctx, argsUp...); err != nil {
			log.Error("xdotool keyup failed", "err", err, "output", string(output))
			return oapi.PressKey500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to release keys. out=%s", string(output))},
			}, nil
		}

		return oapi.PressKey200Response{}, nil
	}

	// Tap behavior: hold modifiers (if any), tap each key, then release modifiers.
	args := []string{}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}
	for _, key := range body.Keys {
		args = append(args, "key", key)
	}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keyup", key)
		}
	}

	log.Info("executing xdotool", "args", args)
	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		return oapi.PressKey500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: fmt.Sprintf("failed to press keys. out=%s", string(output))},
		}, nil
	}
	return oapi.PressKey200Response{}, nil
}

func (s *ApiService) Scroll(ctx context.Context, request oapi.ScrollRequestObject) (oapi.ScrollResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	if request.Body == nil {
		return oapi.Scroll400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"},
		}, nil
	}
	body := *request.Body

	// Validate deltas
	if (body.DeltaX == nil || *body.DeltaX == 0) && (body.DeltaY == nil || *body.DeltaY == 0) {
		return oapi.Scroll400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "at least one of delta_x or delta_y must be non-zero"},
		}, nil
	}

	// Bounds check
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.Scroll500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to get current display resolution"},
		}, nil
	}
	if body.X < 0 || body.Y < 0 {
		return oapi.Scroll400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "coordinates must be non-negative"},
		}, nil
	}
	if body.X >= screenWidth || body.Y >= screenHeight {
		return oapi.Scroll400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)},
		}, nil
	}

	args := []string{}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}
	args = append(args, "mousemove", "--sync", strconv.Itoa(body.X), strconv.Itoa(body.Y))

	// Apply vertical ticks first (sequential as specified)
	if body.DeltaY != nil && *body.DeltaY != 0 {
		count := *body.DeltaY
		btn := "5" // down
		if count < 0 {
			btn = "4" // up
			count = -count
		}
		args = append(args, "click", "--repeat", strconv.Itoa(count), "--delay", "0", btn)
	}
	// Then horizontal ticks
	if body.DeltaX != nil && *body.DeltaX != 0 {
		count := *body.DeltaX
		btn := "7" // right
		if count < 0 {
			btn = "6" // left
			count = -count
		}
		args = append(args, "click", "--repeat", strconv.Itoa(count), "--delay", "0", btn)
	}

	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keyup", key)
		}
	}

	log.Info("executing xdotool", "args", args)
	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool scroll failed", "err", err, "output", string(output))
		return oapi.Scroll500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: fmt.Sprintf("failed to perform scroll: %s", string(output))},
		}, nil
	}
	return oapi.Scroll200Response{}, nil
}

func (s *ApiService) DragMouse(ctx context.Context, request oapi.DragMouseRequestObject) (oapi.DragMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	// serialize input operations to avoid overlapping xdotool commands
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	if request.Body == nil {
		return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "request body is required"}}, nil
	}
	body := *request.Body

	if len(body.Path) < 2 {
		return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: "path must contain at least two points"}}, nil
	}

	// Bounds check for all points
	screenWidth, screenHeight, _, err := s.getCurrentResolution(ctx)
	if err != nil {
		log.Error("failed to get current resolution", "error", err)
		return oapi.DragMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: "failed to get current display resolution"},
		}, nil
	}
	for i, pt := range body.Path {
		if len(pt) != 2 {
			return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: fmt.Sprintf("path[%d] must be [x,y]", i)},
			}, nil
		}
		x := pt[0]
		y := pt[1]
		if x < 0 || y < 0 {
			return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "coordinates must be non-negative"},
			}, nil
		}
		if x >= screenWidth || y >= screenHeight {
			return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: fmt.Sprintf("coordinates exceed screen bounds (max: %dx%d)", screenWidth-1, screenHeight-1)},
			}, nil
		}
	}

	// Button mapping; default to left if unspecified
	btn := "1"
	if body.Button != nil {
		switch *body.Button {
		case oapi.DragMouseRequestButtonLeft:
			btn = "1"
		case oapi.DragMouseRequestButtonMiddle:
			btn = "2"
		case oapi.DragMouseRequestButtonRight:
			btn = "3"
		default:
			return oapi.DragMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: fmt.Sprintf("unsupported button: %s", *body.Button)},
			}, nil
		}
	}

	// Phase 1: keydown modifiers, move to start, mousedown
	args1 := []string{}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args1 = append(args1, "keydown", key)
		}
	}
	start := body.Path[0]
	args1 = append(args1, "mousemove", "--sync", strconv.Itoa(start[0]), strconv.Itoa(start[1]))
	args1 = append(args1, "mousedown", btn)
	log.Info("executing xdotool (drag start)", "args", args1)
	if output, err := defaultXdoTool.Run(ctx, args1...); err != nil {
		log.Error("xdotool drag start failed", "err", err, "output", string(output))
		// Best-effort release modifiers
		if body.HoldKeys != nil {
			argsCleanup := []string{}
			for _, key := range *body.HoldKeys {
				argsCleanup = append(argsCleanup, "keyup", key)
			}
			_, _ = defaultXdoTool.Run(ctx, argsCleanup...)
		}
		return oapi.DragMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: fmt.Sprintf("failed to start drag: %s", string(output))},
		}, nil
	}

	// Optional delay between mousedown and movement
	if body.Delay != nil && *body.Delay > 0 {
		time.Sleep(time.Duration(*body.Delay) * time.Millisecond)
	}

	// Phase 2: move along path (excluding first point) using fixed-count relative steps
	// Insert a small delay between each relative move to smooth the drag
	args2 := []string{}
	// Determine per-segment steps and per-step delay from request (with defaults)
	stepsPerSegment := 10
	if body.StepsPerSegment != nil && *body.StepsPerSegment >= 1 {
		stepsPerSegment = *body.StepsPerSegment
	}
	stepDelayMs := 50
	if body.StepDelayMs != nil && *body.StepDelayMs >= 0 {
		stepDelayMs = *body.StepDelayMs
	}
	stepDelaySeconds := fmt.Sprintf("%.3f", float64(stepDelayMs)/1000.0)

	// Precompute total number of relative steps so we can avoid a trailing sleep
	totalSteps := 0
	prev := start
	for _, pt := range body.Path[1:] {
		x0, y0 := prev[0], prev[1]
		x1, y1 := pt[0], pt[1]
		totalSteps += len(generateRelativeSteps(x1-x0, y1-y0, stepsPerSegment))
		prev = pt
	}

	prev = start
	stepIndex := 0
	for _, pt := range body.Path[1:] {
		x0, y0 := prev[0], prev[1]
		x1, y1 := pt[0], pt[1]
		for _, step := range generateRelativeSteps(x1-x0, y1-y0, stepsPerSegment) {
			xStr := strconv.Itoa(step[0])
			yStr := strconv.Itoa(step[1])
			if step[0] < 0 || step[1] < 0 {
				args2 = append(args2, "mousemove_relative", "--", xStr, yStr)
			} else {
				args2 = append(args2, "mousemove_relative", xStr, yStr)
			}
			// add a tiny delay between moves, but not after the last step
			if stepIndex < totalSteps-1 && stepDelayMs > 0 {
				args2 = append(args2, "sleep", stepDelaySeconds)
			}
			stepIndex++
		}
		prev = pt
	}
	if len(args2) > 0 {
		log.Info("executing xdotool (drag move)", "args", args2)
		if output, err := defaultXdoTool.Run(ctx, args2...); err != nil {
			log.Error("xdotool drag move failed", "err", err, "output", string(output))
			// Try to release button and modifiers
			argsCleanup := []string{"mouseup", btn}
			if body.HoldKeys != nil {
				for _, key := range *body.HoldKeys {
					argsCleanup = append(argsCleanup, "keyup", key)
				}
			}
			_, _ = defaultXdoTool.Run(ctx, argsCleanup...)
			return oapi.DragMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed during drag movement: %s", string(output))},
			}, nil
		}
	}

	// Phase 3: mouseup and release modifiers
	args3 := []string{"mouseup", btn}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args3 = append(args3, "keyup", key)
		}
	}
	log.Info("executing xdotool (drag end)", "args", args3)
	if output, err := defaultXdoTool.Run(ctx, args3...); err != nil {
		log.Error("xdotool drag end failed", "err", err, "output", string(output))
		return oapi.DragMouse500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
			Message: fmt.Sprintf("failed to finish drag: %s", string(output))},
		}, nil
	}

	return oapi.DragMouse200Response{}, nil
}

// generateRelativeSteps produces a sequence of relative steps that approximate a
// straight line from (0,0) to (dx,dy) using at most the provided number of
// steps. Each returned element is a pair {stepX, stepY}. The steps are
// distributed so that the cumulative sum equals exactly (dx, dy). If dx and dy
// are both zero, no steps are returned. If the requested step count is less
// than the distance, the per-step movement will be greater than one pixel.
func generateRelativeSteps(dx, dy, steps int) [][2]int {
	if steps <= 0 {
		return nil
	}
	if dx == 0 && dy == 0 {
		return nil
	}

	out := make([][2]int, 0, steps)

	// Use cumulative rounding to distribute integers across the requested
	// number of steps while preserving the exact totals.
	prevCX := 0
	prevCY := 0
	for i := 1; i <= steps; i++ {
		// Target cumulative positions after i steps
		cx := int(math.Round(float64(i*dx) / float64(steps)))
		cy := int(math.Round(float64(i*dy) / float64(steps)))
		sx := cx - prevCX
		sy := cy - prevCY
		prevCX = cx
		prevCY = cy
		out = append(out, [2]int{sx, sy})
	}

	return out
}
