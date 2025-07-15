package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// XdoTool is a thin wrapper around the xdotool CLI utility. It ensures the
// DISPLAY environment variable is set correctly when invoking xdotool.
//
// Usage:
//
//	output, err := defaultXdoTool.Run(ctx, "mousemove", "100", "100")
//
// If you need a different display, construct your own instance using
// NewXdoTool(display).
type XdoTool struct {
	display string
}

// NewXdoTool returns a new XdoTool configured to target the given X11 display.
// The display string should be in the form ":<num>", e.g. ":0" or ":1".
func NewXdoTool(display string) *XdoTool {
	return &XdoTool{display: display}
}

// defaultXdoTool points to the display we run inside the Docker/unikernel
// environment today. Adjust if the runtime environment changes.
var defaultXdoTool = NewXdoTool(":1")

// Run executes xdotool with the provided arguments. The DISPLAY environment
// variable is injected ahead of the existing environment so that it always
// takes precedence.
func (x *XdoTool) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "xdotool", args...)

	// Prepend/override DISPLAY while preserving the rest of the environment.
	cmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", x.display))

	return cmd.CombinedOutput()
}
