package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// ClipboardHandler is a wrapper that implements http.Handler to handle clipboard requests
type ClipboardHandler struct {
	ApiHandler *ApiService
}

// ServeHTTP handles HTTP requests for clipboard endpoints
func (h *ClipboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/clipboard" && r.Method == http.MethodGet:
		h.GetClipboard(w, r)
	case path == "/clipboard" && r.Method == http.MethodPost:
		h.SetClipboard(w, r)
	case path == "/clipboard/stream" && r.Method == http.MethodGet:
		h.StreamClipboard(w, r)
	default:
		http.NotFound(w, r)
	}
}

// GetClipboard handles the GET /clipboard endpoint
func (h *ClipboardHandler) GetClipboard(w http.ResponseWriter, r *http.Request) {
	res, err := h.ApiHandler.GetClipboard(r.Context(), oapi.GetClipboardRequestObject{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if content, ok := res.(oapi.GetClipboard200JSONResponse); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(content)
	} else {
		http.Error(w, "Unexpected response type", http.StatusInternalServerError)
	}
}

// SetClipboard handles the POST /clipboard endpoint
func (h *ClipboardHandler) SetClipboard(w http.ResponseWriter, r *http.Request) {
	var body oapi.ClipboardContent
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	res, err := h.ApiHandler.SetClipboard(r.Context(), oapi.SetClipboardRequestObject{Body: body})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if content, ok := res.(oapi.SetClipboard200JSONResponse); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(content)
	} else if errResp, ok := res.(oapi.SetClipboard400JSONResponse); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errResp)
	} else if serverErr, ok := res.(oapi.SetClipboard500JSONResponse); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(serverErr)
	} else {
		http.Error(w, "Unexpected response type", http.StatusInternalServerError)
	}
}

// StreamClipboard handles the GET /clipboard/stream endpoint
func (h *ClipboardHandler) StreamClipboard(w http.ResponseWriter, r *http.Request) {
	res, err := h.ApiHandler.StreamClipboard(r.Context(), oapi.StreamClipboardRequestObject{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if stream, ok := res.(*oapi.StreamClipboardResponseStream); ok {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-SSE-Content-Type", stream.Headers.XSSEContentType)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send initial flush
		flusher.Flush()

		// Use a defer to clean up when the stream ends
		defer func() {
			if stream.Cleanup != nil {
				stream.Cleanup()
			}
		}()

		enc := json.NewEncoder(w)
		for event := range stream.Events {
			// Format as SSE
			_, err := w.Write([]byte("event: data\ndata: "))
			if err != nil {
				return
			}

			err = enc.Encode(event)
			if err != nil {
				return
			}

			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return
			}

			flusher.Flush()
		}
	} else {
		http.Error(w, "Unexpected response type", http.StatusInternalServerError)
	}
}

// RegisterClipboardHandlers registers the clipboard endpoints with the router
func RegisterClipboardHandlers(router chi.Router, apiService *ApiService) {
	handler := &ClipboardHandler{ApiHandler: apiService}

	router.Get("/clipboard", handler.GetClipboard)
	router.Post("/clipboard", handler.SetClipboard)
	router.Get("/clipboard/stream", handler.StreamClipboard)
}
