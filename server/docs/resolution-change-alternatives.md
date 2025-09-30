# Alternative Methods for Window Resizing After Resolution Change

This document describes alternative approaches to handle window resizing after changing the display resolution via xrandr, without restarting Chromium.

## Current Implementation

The current implementation in `display.go` restarts Chromium via supervisorctl after a resolution change to ensure the browser window adapts to the new display size. While effective, this approach disrupts the user session.

## Alternative Approaches

### 1. Using xdotool to Resize Windows

**xdotool** is already installed in the image and provides precise window control.

```bash
# Find and resize all Chromium windows to match new resolution
xdotool search --class chromium windowsize %@ 1920 1080

# Or move to origin and then resize
xdotool search --class chromium windowmove %@ 0 0 windowsize %@ 1920 1080
```

**Implementation in Go:**
```go
resizeCmd := []string{"-lc", fmt.Sprintf("xdotool search --class chromium windowmove %%@ 0 0 windowsize %%@ %d %d", width, height)}
resizeEnv := map[string]string{"DISPLAY": display}
resizeReq := oapi.ProcessExecRequest{Command: "bash", Args: &resizeCmd, Env: &resizeEnv}
```

**Pros:**
- No restart needed
- Instant window resize
- Preserves session state
- Already available in the image

**Cons:**
- May not handle all edge cases
- Window decorations might not update properly

### 2. Using wmctrl to Re-maximize Windows

**wmctrl** provides window manager control but needs to be installed first.

```bash
# Install wmctrl
apt-get install -y wmctrl

# Re-maximize all windows
wmctrl -r ':ACTIVE:' -b add,maximized_vert,maximized_horz
```

**Implementation in Go:**
```go
maximizeCmd := []string{"-lc", "wmctrl -r ':ACTIVE:' -b add,maximized_vert,maximized_horz"}
maximizeEnv := map[string]string{"DISPLAY": display}
maximizeReq := oapi.ProcessExecRequest{Command: "bash", Args: &maximizeCmd, Env: &maximizeEnv}
```

**Pros:**
- Works with window manager hints
- Clean maximization
- Respects window manager behavior

**Cons:**
- Requires additional package installation
- May not work if window isn't already maximized

### 3. Toggle Fullscreen Mode

Use xdotool to send F11 key to toggle fullscreen, forcing a re-render.

```bash
# Toggle fullscreen twice to force re-render
xdotool search --class chromium windowactivate && xdotool key F11 && sleep 0.5 && xdotool key F11
```

**Implementation in Go:**
```go
fullscreenCmd := []string{"-lc", "xdotool search --class chromium windowactivate && xdotool key F11 && sleep 0.5 && xdotool key F11"}
fullscreenEnv := map[string]string{"DISPLAY": display}
fullscreenReq := oapi.ProcessExecRequest{Command: "bash", Args: &fullscreenCmd, Env: &fullscreenEnv}
```

**Pros:**
- Forces complete re-render
- Works with Chromium's built-in fullscreen logic
- No additional tools needed

**Cons:**
- Visible flicker during toggle
- May interfere with actual fullscreen state
- Timing dependent

### 4. Using Mutter's D-Bus Interface

Since Mutter is the window manager, its D-Bus interface could be used for window management.

```bash
# This would require more complex D-Bus commands
gdbus call --session \
  --dest org.gnome.Mutter \
  --object-path /org/gnome/Mutter \
  --method org.gnome.Mutter.ResizeWindow
```

**Pros:**
- Native window manager integration
- Most "correct" approach
- Handles all window manager specifics

**Cons:**
- Complex implementation
- D-Bus interface may not be fully exposed
- Requires deeper Mutter knowledge

### 5. JavaScript-based Resize via CDP

Use Chrome DevTools Protocol to resize from within the browser.

```bash
# Send CDP command to resize window
curl -X POST http://localhost:9223/json/runtime/evaluate \
  -d '{"expression": "window.resizeTo(1920, 1080)"}'
```

**Pros:**
- Works from within the browser context
- Can be very precise
- No external window manipulation

**Cons:**
- Requires CDP connection
- May be blocked by browser security
- Only affects browser viewport, not window chrome

## Recommendations

1. **For immediate implementation**: Use xdotool (Option 1) as it's already available and provides good results.

2. **For best user experience**: Implement a combination approach:
   - Try xdotool resize first
   - Fall back to restart if resize fails
   - Add configuration option to choose method

3. **For future enhancement**: 
   - Add wmctrl to the Docker image
   - Implement proper Mutter D-Bus integration
   - Allow users to configure preferred resize method

## Example Enhanced Implementation

```go
func (s *ApiService) resizeChromiumWindow(ctx context.Context, display string, width, height int) error {
    // Try xdotool first
    resizeCmd := []string{"-lc", fmt.Sprintf("xdotool search --class chromium windowmove %%@ 0 0 windowsize %%@ %d %d", width, height)}
    resizeEnv := map[string]string{"DISPLAY": display}
    resizeReq := oapi.ProcessExecRequest{Command: "bash", Args: &resizeCmd, Env: &resizeEnv}
    
    resp, err := s.ProcessExec(ctx, oapi.ProcessExecRequestObject{Body: &resizeReq})
    if err != nil {
        return fmt.Errorf("failed to execute xdotool: %w", err)
    }
    
    if execResp, ok := resp.(oapi.ProcessExec200JSONResponse); ok {
        if execResp.ExitCode != nil && *execResp.ExitCode == 0 {
            return nil // Success
        }
    }
    
    // Fall back to restart if xdotool fails
    return s.restartChromium(ctx)
}
```

## Testing Commands

To test these alternatives manually in a running container:

```bash
# Get into the container
docker exec -it chromium-headful bash

# Test xdotool resize
DISPLAY=:1 xdotool search --class chromium windowsize %@ 1920 1080

# Test fullscreen toggle
DISPLAY=:1 xdotool key F11; sleep 1; xdotool key F11

# Check current window geometry
DISPLAY=:1 xdotool search --class chromium getwindowgeometry
```
