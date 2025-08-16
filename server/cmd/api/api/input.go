package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

// Helper function to map button name to xdotool button number
func buttonNumFromName(button interface{}) string {
	// If already a string that parses as an integer, use it directly
	if buttonStr, ok := button.(string); ok {
		if _, err := strconv.Atoi(buttonStr); err == nil {
			return buttonStr
		}
	}

	// Otherwise, map from name to button code
	buttonMap := map[string]string{
		"left":    "1",
		"middle":  "2",
		"right":   "3",
		"back":    "8",
		"forward": "9",
	}

	if buttonStr, ok := button.(string); ok {
		if btn, exists := buttonMap[strings.ToLower(buttonStr)]; exists {
			return btn
		}
	}

	// If it's an integer, convert to string
	if buttonInt, ok := button.(int); ok {
		return strconv.Itoa(buttonInt)
	}

	// Default to left button
	return "1"
}

// Helper function to find a window ID based on matching criteria
func (s *ApiService) findWindowID(ctx context.Context, match WindowMatch) (string, error) {
	log := logger.FromContext(ctx)
	args := []string{"search"}

	// Build search criteria
	if match.OnlyVisible {
		args = append(args, "--onlyvisible")
	}

	if match.TitleContains != "" {
		args = append(args, "--name", match.TitleContains)
	} else if match.Name != "" {
		args = append(args, "--name", match.Name)
	}

	if match.Class != "" {
		args = append(args, "--class", match.Class)
	}

	if match.Pid != 0 {
		args = append(args, "--pid", strconv.Itoa(match.Pid))
	}

	// Default to all visible windows if no criteria provided
	if len(args) == 1 {
		args = append(args, "--onlyvisible", ".")
	}

	log.Info("searching for window", "args", args)
	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool search failed", "err", err, "output", string(output))
		return "", fmt.Errorf("window search failed: %w", err)
	}

	// Parse the first window ID from the output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no matching windows found")
	}

	return lines[0], nil
}

// Helper function to find multiple window IDs based on matching criteria
func (s *ApiService) findWindowIDs(ctx context.Context, match WindowMatch) ([]string, error) {
	log := logger.FromContext(ctx)
	args := []string{"search"}

	// Build search criteria
	if match.OnlyVisible {
		args = append(args, "--onlyvisible")
	}

	if match.TitleContains != "" {
		args = append(args, "--name", match.TitleContains)
	} else if match.Name != "" {
		args = append(args, "--name", match.Name)
	}

	if match.Class != "" {
		args = append(args, "--class", match.Class)
	}

	if match.Pid != 0 {
		args = append(args, "--pid", strconv.Itoa(match.Pid))
	}

	// Default to all visible windows if no criteria provided
	if len(args) == 1 {
		args = append(args, "--onlyvisible", ".")
	}

	log.Info("searching for windows", "args", args)
	output, err := defaultXdoTool.Run(ctx, args...)
	if err != nil {
		log.Error("xdotool search failed", "err", err, "output", string(output))
		return nil, fmt.Errorf("window search failed: %w", err)
	}

	// Parse window IDs from the output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return []string{}, nil
	}

	return lines, nil
}

// parseXdotoolOutput parses key-value pairs from xdotool output in the format KEY=VALUE
func parseXdotoolOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}

	return result
}

// WindowMatch represents a set of criteria to match windows
type WindowMatch struct {
	TitleContains string `json:"title_contains,omitempty"`
	Name          string `json:"name,omitempty"`
	Class         string `json:"class,omitempty"`
	Pid           int    `json:"pid,omitempty"`
	OnlyVisible   bool   `json:"only_visible,omitempty"`
}

// MouseMoveRequest represents the request to move the mouse cursor
type MouseMoveRequest struct {
	X        int      `json:"x"`
	Y        int      `json:"y"`
	HoldKeys []string `json:"hold_keys,omitempty"`
}

// MouseMoveRelativeRequest represents the request to move the mouse cursor relative to its current position
type MouseMoveRelativeRequest struct {
	Dx int `json:"dx,omitempty"`
	Dy int `json:"dy,omitempty"`
}

// MouseClickRequest represents the request to click a mouse button
type MouseClickRequest struct {
	Button interface{} `json:"button"` // Can be string or int
	Count  int         `json:"count,omitempty"`
}

// MouseButtonRequest represents the request to press or release a mouse button
type MouseButtonRequest struct {
	Button interface{} `json:"button"` // Can be string or int
}

// MouseScrollRequest represents the request to scroll the mouse wheel
type MouseScrollRequest struct {
	Dx int `json:"dx,omitempty"`
	Dy int `json:"dy,omitempty"`
}

// MouseLocationResponse represents the response with the current mouse cursor location
type MouseLocationResponse struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Screen int    `json:"screen"`
	Window string `json:"window,omitempty"`
}

// KeyboardTypeRequest represents the request to type text
type KeyboardTypeRequest struct {
	Text  string `json:"text"`
	Wpm   int    `json:"wpm,omitempty"`
	Enter bool   `json:"enter,omitempty"`
}

// KeyRequest represents the request to press or release a key
type KeyRequest struct {
	Key string `json:"key"`
}

// KeyboardKeysRequest represents the request to send key presses
type KeyboardKeysRequest struct {
	Keys []string `json:"keys"`
}

// WindowMatchRequest represents the request to find a window by match criteria
type WindowMatchRequest struct {
	Match WindowMatch `json:"match"`
}

// WindowIdRequest represents the request to operate on a window by ID
type WindowIdRequest struct {
	Wid string `json:"wid"`
}

// WindowActivateResponse represents the response after activating a window
type WindowActivateResponse struct {
	Activated bool   `json:"activated"`
	Wid       string `json:"wid,omitempty"`
}

// WindowFocusResponse represents the response after focusing a window
type WindowFocusResponse struct {
	Focused bool   `json:"focused"`
	Wid     string `json:"wid,omitempty"`
}

// WindowCloseResponse represents the response after closing a window
type WindowCloseResponse struct {
	Ok        bool     `json:"ok"`
	Wid       string   `json:"wid,omitempty"`
	WindowIds []string `json:"windowIds,omitempty"`
}

// ActiveWindowResponse represents the response with the active window
type ActiveWindowResponse struct {
	Wid string `json:"wid"`
}

// WindowNameResponse represents the response with the window name
type WindowNameResponse struct {
	Wid  string `json:"wid"`
	Name string `json:"name"`
}

// WindowPidResponse represents the response with the window process ID
type WindowPidResponse struct {
	Wid string `json:"wid"`
	Pid int    `json:"pid"`
}

// WindowGeometryResponse represents the response with the window geometry
type WindowGeometryResponse struct {
	Wid    string `json:"wid"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Screen int    `json:"screen"`
}

// DisplayGeometryResponse represents the response with the display geometry
type DisplayGeometryResponse struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowMoveResizeRequest represents the request to move and resize a window
type WindowMoveResizeRequest struct {
	Match  WindowMatch `json:"match"`
	X      *int        `json:"x,omitempty"`
	Y      *int        `json:"y,omitempty"`
	Width  *int        `json:"width,omitempty"`
	Height *int        `json:"height,omitempty"`
}

// DesktopCountRequest represents the request to set the number of desktops
type DesktopCountRequest struct {
	Count int `json:"count"`
}

// DesktopCountResponse represents the response with the number of desktops
type DesktopCountResponse struct {
	Count int `json:"count"`
}

// DesktopIndexRequest represents the request to set the current desktop
type DesktopIndexRequest struct {
	Index int `json:"index"`
}

// DesktopIndexResponse represents the response with the current desktop index
type DesktopIndexResponse struct {
	Index int `json:"index"`
}

// WindowDesktopRequest represents the request to move a window to a desktop
type WindowDesktopRequest struct {
	Match WindowMatch `json:"match"`
	Index int         `json:"index"`
}

// DesktopViewportRequest represents the request to set the desktop viewport
type DesktopViewportRequest struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// DesktopViewportResponse represents the response with the desktop viewport
type DesktopViewportResponse struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ActivateAndTypeRequest represents the request to activate a window and type text
type ActivateAndTypeRequest struct {
	Match WindowMatch `json:"match"`
	Text  string      `json:"text,omitempty"`
	Enter bool        `json:"enter,omitempty"`
	Wpm   int         `json:"wpm,omitempty"`
}

// ActivateAndKeysRequest represents the request to activate a window and send key presses
type ActivateAndKeysRequest struct {
	Match WindowMatch `json:"match"`
	Keys  []string    `json:"keys"`
}

// ComboWindowResponse represents the response after a combo action on a window
type ComboWindowResponse struct {
	Ok  bool   `json:"ok"`
	Wid string `json:"wid"`
}

// WindowCenterRequest represents the request to center a window
type WindowCenterRequest struct {
	Match  WindowMatch `json:"match"`
	Width  *int        `json:"width,omitempty"`
	Height *int        `json:"height,omitempty"`
}

// WindowCenterResponse represents the response after centering a window
type WindowCenterResponse struct {
	Ok     bool   `json:"ok"`
	Wid    string `json:"wid"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// WindowSnapRequest represents the request to snap a window to a position
type WindowSnapRequest struct {
	Match    WindowMatch `json:"match"`
	Position string      `json:"position"`
}

// WindowSnapResponse represents the response after snapping a window
type WindowSnapResponse struct {
	Ok     bool   `json:"ok"`
	Wid    string `json:"wid"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SystemExecRequest represents the request to execute a system command
type SystemExecRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// SystemExecResponse represents the response after executing a system command
type SystemExecResponse struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// SleepRequest represents the request to sleep for a duration
type SleepRequest struct {
	Seconds float64 `json:"seconds"`
}

// OkResponse represents a simple success response
type OkResponse struct {
	Ok bool `json:"ok"`
}
