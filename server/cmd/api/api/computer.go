// server/cmd/api/api/computer.go
package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func (s *ApiService) MoveMouse(ctx context.Context, request oapi.MoveMouseRequestObject) (oapi.MoveMouseResponseObject, error) {
	log := logger.FromContext(ctx)
	if request.Body == nil {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body
	if body.X < 0 || body.Y < 0 {
		return oapi.MoveMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
	}
	args := []string{}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}
	args = append(args, "mousemove", "--sync", strconv.Itoa(body.X), strconv.Itoa(body.Y))
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
	if request.Body == nil {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body is required"}}, nil
	}
	body := *request.Body
	if body.X < 0 || body.Y < 0 {
		return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "coordinates must be non-negative"}}, nil
	}
	btn := "1"
	if body.Button != nil {
		buttonMap := map[oapi.ClickMouseRequestButton]string{
			oapi.Left:    "1",
			oapi.Middle:  "2",
			oapi.Right:   "3",
			oapi.Back:    "8",
			oapi.Forward: "9",
		}
		if m, ok := buttonMap[*body.Button]; ok {
			btn = m
		} else {
			return oapi.ClickMouse400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: fmt.Sprintf("unsupported button: %s", *body.Button)}}, nil
		}
	}
	numClicks := 1
	if body.NumClicks != nil && *body.NumClicks > 0 {
		numClicks = *body.NumClicks
	}
	args := []string{}
	if body.HoldKeys != nil {
		for _, key := range *body.HoldKeys {
			args = append(args, "keydown", key)
		}
	}
	args = append(args, "mousemove", "--sync", strconv.Itoa(body.X), strconv.Itoa(body.Y))
	clickType := oapi.Click
	if body.ClickType != nil {
		clickType = *body.ClickType
	}
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

func (s *ApiService) PasteClipboard(ctx context.Context, request oapi.PasteClipboardRequestObject) (oapi.PasteClipboardResponseObject, error) {
    log := logger.FromContext(ctx)
    if request.Body == nil || request.Body.Text == "" {
        return oapi.PasteClipboard400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "text is required"}}, nil
    }
    text := request.Body.Text
    clip := exec.Command("bash", "-c", fmt.Sprintf("printf %%s %q | xclip -selection clipboard", text))
    clip.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", defaultXdoTool.display))
    if out, err := clip.CombinedOutput(); err != nil {
        log.Error("failed to set clipboard", "err", err, "output", string(out))
        return oapi.PasteClipboard500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to set clipboard"}}, nil
    }
    if _, err := defaultXdoTool.Run(ctx, "key", "ctrl+v"); err != nil {
        log.Error("failed to paste text", "err", err)
        return oapi.PasteClipboard500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to paste text"}}, nil
    }
    return oapi.PasteClipboard200Response{}, nil
}
