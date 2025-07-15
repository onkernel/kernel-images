package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func (s *ApiService) MoveMouse(ctx context.Context, request oapi.MoveMouseRequestObject) (oapi.MoveMouseResponseObject, error) {
	log := logger.FromContext(ctx)

	// Validate request body
	if request.Body == nil {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body

	// Ensure non-negative coordinates
	if body.X < 0 || body.Y < 0 {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
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

	// Validate request body
	if request.Body == nil {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body

	// Ensure non-negative coordinates
	if body.X < 0 || body.Y < 0 {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
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
