package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

// InputMouseMove handles the /input/mouse/move endpoint
func (s *ApiService) InputMouseMove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.X < 0 || req.Y < 0 {
		http.Error(w, "coordinates must be non-negative", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{}

	// Hold modifier keys (keydown)
	if len(req.HoldKeys) > 0 {
		for _, key := range req.HoldKeys {
			args = append(args, "keydown", key)
		}
	}

	// Move the cursor to the desired coordinates
	args = append(args, "mousemove", "--sync", strconv.Itoa(req.X), strconv.Itoa(req.Y))

	// Release modifier keys (keyup)
	if len(req.HoldKeys) > 0 {
		for _, key := range req.HoldKeys {
			args = append(args, "keyup", key)
		}
	}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to move mouse", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseMoveRelative handles the /input/mouse/move_relative endpoint
func (s *ApiService) InputMouseMoveRelative(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseMoveRelativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"mousemove_relative", "--", strconv.Itoa(req.Dx), strconv.Itoa(req.Dy)}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to move mouse relatively", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseClick handles the /input/mouse/click endpoint
func (s *ApiService) InputMouseClick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseClickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Set default count if not provided
	count := req.Count
	if count <= 0 {
		count = 1
	}

	// Convert button to xdotool format
	button := buttonNumFromName(req.Button)

	// Build xdotool arguments
	args := []string{"click"}
	if count > 1 {
		args = append(args, "--repeat", strconv.Itoa(count))
	}
	args = append(args, button)

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to click mouse", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseDown handles the /input/mouse/down endpoint
func (s *ApiService) InputMouseDown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseButtonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Convert button to xdotool format
	button := buttonNumFromName(req.Button)

	// Build xdotool arguments
	args := []string{"mousedown", button}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to press mouse button", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseUp handles the /input/mouse/up endpoint
func (s *ApiService) InputMouseUp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseButtonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Convert button to xdotool format
	button := buttonNumFromName(req.Button)

	// Build xdotool arguments
	args := []string{"mouseup", button}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to release mouse button", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseScroll handles the /input/mouse/scroll endpoint
func (s *ApiService) InputMouseScroll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req MouseScrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Calculate scroll clicks based on delta values
	verticalClicks := 0
	horizontalClicks := 0
	if req.Dy != 0 {
		verticalClicks = max(1, abs(req.Dy)/120)
	}
	if req.Dx != 0 {
		horizontalClicks = max(1, abs(req.Dx)/120)
	}

	// Determine button numbers based on direction
	verticalButton := "5" // Scroll down
	if req.Dy < 0 {
		verticalButton = "4" // Scroll up
	}

	horizontalButton := "7" // Scroll right
	if req.Dx < 0 {
		horizontalButton = "6" // Scroll left
	}

	// Execute scroll commands
	for i := 0; i < verticalClicks; i++ {
		output, err := defaultXdoTool.Run(ctx, "click", verticalButton)
		if err != nil {
			log.Error("xdotool vertical scroll failed", "err", err, "output", string(output))
			http.Error(w, "failed to scroll vertically", http.StatusInternalServerError)
			return
		}
	}

	for i := 0; i < horizontalClicks; i++ {
		output, err := defaultXdoTool.Run(ctx, "click", horizontalButton)
		if err != nil {
			log.Error("xdotool horizontal scroll failed", "err", err, "output", string(output))
			http.Error(w, "failed to scroll horizontally", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputMouseLocation handles the /input/mouse/location endpoint
func (s *ApiService) InputMouseLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Build xdotool arguments
	args := []string{"getmouselocation", "--shell"}

	log.Info("executing xdotool", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get mouse location", http.StatusInternalServerError)
		return
	}

	// Parse output
	kv := parseXdotoolOutput(string(output))

	response := MouseLocationResponse{}
	if x, ok := kv["X"]; ok {
		if xVal, err := strconv.Atoi(x); err == nil {
			response.X = xVal
		}
	}
	if y, ok := kv["Y"]; ok {
		if yVal, err := strconv.Atoi(y); err == nil {
			response.Y = yVal
		}
	}
	if screen, ok := kv["SCREEN"]; ok {
		if screenVal, err := strconv.Atoi(screen); err == nil {
			response.Screen = screenVal
		}
	}
	if window, ok := kv["WINDOW"]; ok {
		response.Window = window
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// InputKeyboardType handles the /input/keyboard/type endpoint
func (s *ApiService) InputKeyboardType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req KeyboardTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	// Calculate typing delay based on WPM
	wpm := req.Wpm
	if wpm <= 0 {
		wpm = 300 // Default WPM
	}
	delay := max(1, 60000/(wpm*5)) // Convert WPM to delay in ms

	// Build xdotool arguments for typing
	args := []string{"type", "--delay", strconv.Itoa(delay), "--clearmodifiers", "--", req.Text}

	log.Info("executing xdotool type", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool type command failed", "err", err, "output", string(output))
		http.Error(w, "failed to type text", http.StatusInternalServerError)
		return
	}

	// Press Enter if requested
	if req.Enter {
		enterOutput, enterErr := defaultXdoTool.Run(ctx, "key", "Return")
		if enterErr != nil {
			log.Error("xdotool enter key failed", "err", enterErr, "output", string(enterOutput))
			http.Error(w, "failed to press Enter", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputKeyboardKey handles the /input/keyboard/key endpoint
func (s *ApiService) InputKeyboardKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req KeyboardKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Keys) == 0 {
		http.Error(w, "keys are required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"key"}
	args = append(args, req.Keys...)

	log.Info("executing xdotool key", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool key command failed", "err", err, "output", string(output))
		http.Error(w, "failed to press keys", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputKeyboardKeyDown handles the /input/keyboard/key_down endpoint
func (s *ApiService) InputKeyboardKeyDown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req KeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"keydown", req.Key}

	log.Info("executing xdotool keydown", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool keydown command failed", "err", err, "output", string(output))
		http.Error(w, "failed to press key down", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputKeyboardKeyUp handles the /input/keyboard/key_up endpoint
func (s *ApiService) InputKeyboardKeyUp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req KeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"keyup", req.Key}

	log.Info("executing xdotool keyup", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool keyup command failed", "err", err, "output", string(output))
		http.Error(w, "failed to release key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowActivate handles the /input/window/activate endpoint
func (s *ApiService) InputWindowActivate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Info("window not found", "err", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WindowActivateResponse{Activated: false})
		return
	}

	// Build xdotool arguments
	args := []string{"windowactivate", wid}

	log.Info("executing xdotool windowactivate", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowactivate command failed", "err", err, "output", string(output))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WindowActivateResponse{Activated: false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WindowActivateResponse{Activated: true, Wid: wid})
}

// InputWindowFocus handles the /input/window/focus endpoint
func (s *ApiService) InputWindowFocus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Info("window not found", "err", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WindowFocusResponse{Focused: false})
		return
	}

	// Build xdotool arguments
	args := []string{"windowfocus", wid}

	log.Info("executing xdotool windowfocus", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowfocus command failed", "err", err, "output", string(output))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WindowFocusResponse{Focused: false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WindowFocusResponse{Focused: true, Wid: wid})
}

// InputWindowMoveResize handles the /input/window/move_resize endpoint
func (s *ApiService) InputWindowMoveResize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMoveResizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Move window if coordinates provided
	if req.X != nil && req.Y != nil {
		moveArgs := []string{"windowmove", wid, strconv.Itoa(*req.X), strconv.Itoa(*req.Y)}
		log.Info("executing xdotool windowmove", "args", moveArgs)

		moveOutput, moveErr := defaultXdoTool.Run(ctx, moveArgs...)
		if moveErr != nil {
			log.Error("xdotool windowmove command failed", "err", moveErr, "output", string(moveOutput))
			http.Error(w, "failed to move window", http.StatusInternalServerError)
			return
		}
	}

	// Resize window if dimensions provided
	if req.Width != nil && req.Height != nil {
		sizeArgs := []string{"windowsize", wid, strconv.Itoa(*req.Width), strconv.Itoa(*req.Height)}
		log.Info("executing xdotool windowsize", "args", sizeArgs)

		sizeOutput, sizeErr := defaultXdoTool.Run(ctx, sizeArgs...)
		if sizeErr != nil {
			log.Error("xdotool windowsize command failed", "err", sizeErr, "output", string(sizeOutput))
			http.Error(w, "failed to resize window", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowRaise handles the /input/window/raise endpoint
func (s *ApiService) InputWindowRaise(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Build xdotool arguments
	args := []string{"windowraise", wid}

	log.Info("executing xdotool windowraise", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowraise command failed", "err", err, "output", string(output))
		http.Error(w, "failed to raise window", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowMinimize handles the /input/window/minimize endpoint
func (s *ApiService) InputWindowMinimize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Build xdotool arguments
	args := []string{"windowminimize", wid}

	log.Info("executing xdotool windowminimize", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowminimize command failed", "err", err, "output", string(output))
		http.Error(w, "failed to minimize window", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowMap handles the /input/window/map endpoint
func (s *ApiService) InputWindowMap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Build xdotool arguments
	args := []string{"windowmap", wid}

	log.Info("executing xdotool windowmap", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowmap command failed", "err", err, "output", string(output))
		http.Error(w, "failed to map window", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowUnmap handles the /input/window/unmap endpoint
func (s *ApiService) InputWindowUnmap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Build xdotool arguments
	args := []string{"windowunmap", wid}

	log.Info("executing xdotool windowunmap", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowunmap command failed", "err", err, "output", string(output))
		http.Error(w, "failed to unmap window", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowClose handles the /input/window/close endpoint
func (s *ApiService) InputWindowClose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window IDs
	windowIds, err := s.findWindowIDs(ctx, req.Match)
	if err != nil || len(windowIds) == 0 {
		log.Error("windows not found", "err", err)
		http.Error(w, "no matching windows found", http.StatusNotFound)
		return
	}

	// Close each window
	for _, wid := range windowIds {
		args := []string{"windowclose", wid}
		log.Info("executing xdotool windowclose", "args", args)

		output, err := defaultXdoTool.Run(ctx, args...)
		if err != nil {
			log.Error("xdotool windowclose command failed", "err", err, "output", string(output), "wid", wid)
			// Continue with other windows even if one fails
		}
	}

	response := WindowCloseResponse{
		Ok:        true,
		WindowIds: windowIds,
	}
	if len(windowIds) == 1 {
		response.Wid = windowIds[0]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// InputWindowKill handles the /input/window/kill endpoint
func (s *ApiService) InputWindowKill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Find window ID
	wid, err := s.findWindowID(ctx, req.Match)
	if err != nil {
		log.Error("window not found", "err", err)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}

	// Build xdotool arguments
	args := []string{"windowkill", wid}

	log.Info("executing xdotool windowkill", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool windowkill command failed", "err", err, "output", string(output))
		http.Error(w, "failed to kill window", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OkResponse{Ok: true})
}

// InputWindowActive handles the /input/window/active endpoint
func (s *ApiService) InputWindowActive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Build xdotool arguments
	args := []string{"getactivewindow"}

	log.Info("executing xdotool getactivewindow", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getactivewindow command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get active window", http.StatusInternalServerError)
		return
	}

	wid := strings.TrimSpace(string(output))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ActiveWindowResponse{Wid: wid})
}

// InputWindowFocused handles the /input/window/focused endpoint
func (s *ApiService) InputWindowFocused(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Build xdotool arguments
	args := []string{"getwindowfocus"}

	log.Info("executing xdotool getwindowfocus", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getwindowfocus command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get focused window", http.StatusInternalServerError)
		return
	}

	wid := strings.TrimSpace(string(output))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ActiveWindowResponse{Wid: wid})
}

// InputWindowName handles the /input/window/name endpoint
func (s *ApiService) InputWindowName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Wid == "" {
		http.Error(w, "window ID is required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"getwindowname", req.Wid}

	log.Info("executing xdotool getwindowname", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getwindowname command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get window name", http.StatusInternalServerError)
		return
	}

	name := strings.TrimSpace(string(output))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WindowNameResponse{Wid: req.Wid, Name: name})
}

// InputWindowPid handles the /input/window/pid endpoint
func (s *ApiService) InputWindowPid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Wid == "" {
		http.Error(w, "window ID is required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"getwindowpid", req.Wid}

	log.Info("executing xdotool getwindowpid", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getwindowpid command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get window PID", http.StatusInternalServerError)
		return
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		log.Error("failed to parse PID", "err", err, "output", pidStr)
		http.Error(w, "failed to parse window PID", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WindowPidResponse{Wid: req.Wid, Pid: pid})
}

// InputWindowGeometry handles the /input/window/geometry endpoint
func (s *ApiService) InputWindowGeometry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req WindowIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request body", "err", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Wid == "" {
		http.Error(w, "window ID is required", http.StatusBadRequest)
		return
	}

	// Build xdotool arguments
	args := []string{"getwindowgeometry", "--shell", req.Wid}

	log.Info("executing xdotool getwindowgeometry", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getwindowgeometry command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get window geometry", http.StatusInternalServerError)
		return
	}

	// Parse output
	kv := parseXdotoolOutput(string(output))

	response := WindowGeometryResponse{Wid: req.Wid}
	if x, ok := kv["X"]; ok {
		if xVal, err := strconv.Atoi(x); err == nil {
			response.X = xVal
		}
	}
	if y, ok := kv["Y"]; ok {
		if yVal, err := strconv.Atoi(y); err == nil {
			response.Y = yVal
		}
	}
	if width, ok := kv["WIDTH"]; ok {
		if widthVal, err := strconv.Atoi(width); err == nil {
			response.Width = widthVal
		}
	}
	if height, ok := kv["HEIGHT"]; ok {
		if heightVal, err := strconv.Atoi(height); err == nil {
			response.Height = heightVal
		}
	}
	if screen, ok := kv["SCREEN"]; ok {
		if screenVal, err := strconv.Atoi(screen); err == nil {
			response.Screen = screenVal
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// InputDisplayGeometry handles the /input/display/geometry endpoint
func (s *ApiService) InputDisplayGeometry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Build xdotool arguments
	args := []string{"getdisplaygeometry"}

	log.Info("executing xdotool getdisplaygeometry", "args", args)

	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool getdisplaygeometry command failed", "err", err, "output", string(output))
		http.Error(w, "failed to get display geometry", http.StatusInternalServerError)
		return
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		log.Error("unexpected output format from getdisplaygeometry", "output", string(output))
		http.Error(w, "unexpected output format from xdotool", http.StatusInternalServerError)
		return
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		log.Error("failed to parse display width", "err", err, "width", parts[0])
		http.Error(w, "failed to parse display width", http.StatusInternalServerError)
		return
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Error("failed to parse display height", "err", err, "height", parts[1])
		http.Error(w, "failed to parse display height", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DisplayGeometryResponse{Width: width, Height: height})
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
