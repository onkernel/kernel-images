package oapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ClipboardContentType Type of clipboard content
type ClipboardContentType string

// Defines values for ClipboardContentType.
const (
	Text  ClipboardContentType = "text"
	Image ClipboardContentType = "image"
)

// ClipboardContent defines model for ClipboardContent.
type ClipboardContent struct {
	// Type Type of clipboard content
	Type ClipboardContentType `json:"type"`

	// Text Text content when type is 'text'
	Text *string `json:"text,omitempty"`

	// ImageB64 Base64-encoded image content when type is 'image'
	ImageB64 *string `json:"image_b64,omitempty"`

	// ImageMime MIME type of the image when type is 'image'
	ImageMime *string `json:"image_mime,omitempty"`
}

// ClipboardSetResponse defines model for ClipboardSetResponse.
type ClipboardSetResponse struct {
	// Ok Whether the clipboard was set successfully
	Ok bool `json:"ok"`

	// Message Error message if clipboard couldn't be set
	Message *string `json:"message,omitempty"`
}

// ClipboardEvent defines model for ClipboardEvent.
type ClipboardEvent struct {
	// Ts Timestamp of the clipboard change
	Ts string `json:"ts"`

	// Type Type of clipboard content
	Type ClipboardContentType `json:"type"`

	// Preview Preview of the clipboard content (truncated if text, or 'image...' if an image)
	Preview *string `json:"preview,omitempty"`
}

// GetClipboardRequestObject provides context for handlers
type GetClipboardRequestObject struct{}

// SetClipboardRequestObject provides context for handlers
type SetClipboardRequestObject struct {
	Body ClipboardContent
}

// StreamClipboardRequestObject provides context for handlers
type StreamClipboardRequestObject struct{}

// GetClipboard200JSONResponse is returned on successful get operation
type GetClipboard200JSONResponse = ClipboardContent

// SetClipboard200JSONResponse is returned on successful set operation
type SetClipboard200JSONResponse = ClipboardSetResponse

// SetClipboard400JSONResponse is returned on bad request
type SetClipboard400JSONResponse struct {
	BadRequestErrorJSONResponse
}

// SetClipboard500JSONResponse is returned on internal error
type SetClipboard500JSONResponse struct {
	InternalErrorJSONResponse
}

// StreamClipboard200ResponseHeaders defines headers for StreamClipboard200 response.
type StreamClipboard200ResponseHeaders struct {
	XSSEContentType string `json:"X-SSE-Content-Type"`
}

// StreamClipboardResponseStream is the streaming response for clipboard events
type StreamClipboardResponseStream struct {
	Headers StreamClipboard200ResponseHeaders
	Events  chan ClipboardEvent
	Cleanup func()
}

// GetClipboardResponseObject defines the different possible responses for GetClipboard.
type GetClipboardResponseObject interface {
	visitGetClipboardResponse(w http.ResponseWriter) error
}

// SetClipboardResponseObject defines the different possible responses for SetClipboard.
type SetClipboardResponseObject interface {
	visitSetClipboardResponse(w http.ResponseWriter) error
}

// StreamClipboardResponseObject defines the different possible responses for StreamClipboard.
type StreamClipboardResponseObject interface {
	visitStreamClipboardResponse(w http.ResponseWriter) error
}

func (response GetClipboard200JSONResponse) visitGetClipboardResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

func (response SetClipboard200JSONResponse) visitSetClipboardResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

func (response SetClipboard400JSONResponse) visitSetClipboardResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)

	return json.NewEncoder(w).Encode(response)
}

func (response SetClipboard500JSONResponse) visitSetClipboardResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(500)

	return json.NewEncoder(w).Encode(response)
}

func (response *StreamClipboardResponseStream) visitStreamClipboardResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-SSE-Content-Type", response.Headers.XSSEContentType)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	enc := json.NewEncoder(w)

	// Send initial flush
	flusher.Flush()

	// Use a defer to clean up when the stream ends
	defer func() {
		if response.Cleanup != nil {
			response.Cleanup()
		}
	}()

	for event := range response.Events {
		// Format as SSE
		fmt.Fprintf(w, "event: data\ndata: ")
		enc.Encode(event)
		fmt.Fprintf(w, "\n\n")
		flusher.Flush()
	}

	return nil
}
