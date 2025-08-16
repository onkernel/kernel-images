package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// RegisterInputAPIHandlers registers the input API handlers with the provided ServeMux
func (s *ApiService) RegisterInputAPIHandlers(mux chi.Router) {
	// Mouse operations
	mux.HandleFunc("/input/mouse/move", s.InputMouseMove)
	mux.HandleFunc("/input/mouse/move_relative", s.InputMouseMoveRelative)
	mux.HandleFunc("/input/mouse/click", s.InputMouseClick)
	mux.HandleFunc("/input/mouse/down", s.InputMouseDown)
	mux.HandleFunc("/input/mouse/up", s.InputMouseUp)
	mux.HandleFunc("/input/mouse/scroll", s.InputMouseScroll)
	mux.HandleFunc("/input/mouse/location", s.InputMouseLocation)

	// Keyboard operations
	mux.HandleFunc("/input/keyboard/type", s.InputKeyboardType)
	mux.HandleFunc("/input/keyboard/key", s.InputKeyboardKey)
	mux.HandleFunc("/input/keyboard/key_down", s.InputKeyboardKeyDown)
	mux.HandleFunc("/input/keyboard/key_up", s.InputKeyboardKeyUp)

	// Window operations
	mux.HandleFunc("/input/window/activate", s.InputWindowActivate)
	mux.HandleFunc("/input/window/focus", s.InputWindowFocus)
	mux.HandleFunc("/input/window/move_resize", s.InputWindowMoveResize)
	mux.HandleFunc("/input/window/raise", s.InputWindowRaise)
	mux.HandleFunc("/input/window/minimize", s.InputWindowMinimize)
	mux.HandleFunc("/input/window/map", s.InputWindowMap)
	mux.HandleFunc("/input/window/unmap", s.InputWindowUnmap)
	mux.HandleFunc("/input/window/close", s.InputWindowClose)
	mux.HandleFunc("/input/window/kill", s.InputWindowKill)
	mux.HandleFunc("/input/window/active", s.InputWindowActive)
	mux.HandleFunc("/input/window/focused", s.InputWindowFocused)
	mux.HandleFunc("/input/window/name", s.InputWindowName)
	mux.HandleFunc("/input/window/pid", s.InputWindowPid)
	mux.HandleFunc("/input/window/geometry", s.InputWindowGeometry)

	// Display and desktop operations
	mux.HandleFunc("/input/display/geometry", s.InputDisplayGeometry)

	// Also register the legacy endpoint handlers for backward compatibility
	mux.HandleFunc("/computer/move_mouse", func(w http.ResponseWriter, r *http.Request) {
		s.InputMouseMove(w, r)
	})

	mux.HandleFunc("/computer/click_mouse", func(w http.ResponseWriter, r *http.Request) {
		s.InputMouseClick(w, r)
	})

	// Add a wrapper for the health check handler
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		uptimeSec := int(time.Since(s.startTime).Seconds())
		w.Write([]byte(fmt.Sprintf(`{"status":"ok","uptime_sec":%d}`, uptimeSec)))
	})
}
