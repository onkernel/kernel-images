package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputMouseMove(t *testing.T) {
	// Create a new API service instance
	s := &ApiService{
		startTime: time.Now(),
	}

	// Test case: valid mouse move request
	t.Run("valid request", func(t *testing.T) {
		// Create a request body
		reqBody := MouseMoveRequest{
			X: 100,
			Y: 200,
		}
		reqBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// Create a request
		req, err := http.NewRequest("POST", "/input/mouse/move", bytes.NewBuffer(reqBytes))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler := http.HandlerFunc(s.InputMouseMove)
		handler.ServeHTTP(rr, req)

		// Check the status code
		assert.Equal(t, http.StatusOK, rr.Code)

		// The actual xdotool execution will be mocked or skipped in test
		// As long as the handler doesn't panic, the test passes
	})

	// Test case: invalid request (negative coordinates)
	t.Run("invalid request - negative coordinates", func(t *testing.T) {
		// Create a request body with negative coordinates
		reqBody := MouseMoveRequest{
			X: -100,
			Y: -200,
		}
		reqBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// Create a request
		req, err := http.NewRequest("POST", "/input/mouse/move", bytes.NewBuffer(reqBytes))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler := http.HandlerFunc(s.InputMouseMove)
		handler.ServeHTTP(rr, req)

		// Check the status code (should be 400 Bad Request)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestInputMouseClick(t *testing.T) {
	// Create a new API service instance
	s := &ApiService{
		startTime: time.Now(),
	}

	// Test case: valid mouse click request
	t.Run("valid request", func(t *testing.T) {
		// Create a request body
		reqBody := MouseClickRequest{
			Button: "left",
			Count:  1,
		}
		reqBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// Create a request
		req, err := http.NewRequest("POST", "/input/mouse/click", bytes.NewBuffer(reqBytes))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler := http.HandlerFunc(s.InputMouseClick)
		handler.ServeHTTP(rr, req)

		// Check the status code
		assert.Equal(t, http.StatusOK, rr.Code)

		// The actual xdotool execution will be mocked or skipped in test
		// As long as the handler doesn't panic, the test passes
	})
}

func TestInputKeyboardType(t *testing.T) {
	// Create a new API service instance
	s := &ApiService{
		startTime: time.Now(),
	}

	// Test case: valid keyboard type request
	t.Run("valid request", func(t *testing.T) {
		// Create a request body
		reqBody := KeyboardTypeRequest{
			Text:  "Hello, World!",
			Wpm:   300,
			Enter: true,
		}
		reqBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// Create a request
		req, err := http.NewRequest("POST", "/input/keyboard/type", bytes.NewBuffer(reqBytes))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		// Create a response recorder
		rr := httptest.NewRecorder()

		// Call the handler
		handler := http.HandlerFunc(s.InputKeyboardType)
		handler.ServeHTTP(rr, req)

		// Check the status code
		assert.Equal(t, http.StatusOK, rr.Code)

		// The actual xdotool execution will be mocked or skipped in test
		// As long as the handler doesn't panic, the test passes
	})
}

func TestButtonNumFromName(t *testing.T) {
	// Test string names
	assert.Equal(t, "1", buttonNumFromName("left"))
	assert.Equal(t, "2", buttonNumFromName("middle"))
	assert.Equal(t, "3", buttonNumFromName("right"))
	assert.Equal(t, "8", buttonNumFromName("back"))
	assert.Equal(t, "9", buttonNumFromName("forward"))

	// Test numeric values
	assert.Equal(t, "5", buttonNumFromName(5))

	// Test unknown/default
	assert.Equal(t, "1", buttonNumFromName("unknown"))
	assert.Equal(t, "1", buttonNumFromName(nil))
}
