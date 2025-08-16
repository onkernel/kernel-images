package api

import (
	"context"
	"testing"
	"time"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/stretchr/testify/assert"
)

func TestClipboardContentTypes(t *testing.T) {
	// Simple test to verify the clipboard content types are defined correctly
	assert.Equal(t, oapi.ClipboardContentType("text"), oapi.Text)
	assert.Equal(t, oapi.ClipboardContentType("image"), oapi.Image)
}

// Mock clipboard manager for testing
type mockClipboardManager struct {
	getTextFunc func(ctx context.Context) (string, error)
	setTextFunc func(ctx context.Context, text string) error
}

func (m *mockClipboardManager) getClipboardText(ctx context.Context) (string, error) {
	if m.getTextFunc != nil {
		return m.getTextFunc(ctx)
	}
	return "", nil
}

func (m *mockClipboardManager) setClipboardText(ctx context.Context, text string) error {
	if m.setTextFunc != nil {
		return m.setTextFunc(ctx, text)
	}
	return nil
}

// Simple test for clipboard response creation
func TestClipboardResponseTypes(t *testing.T) {
	// Test clipboard content response creation
	text := "Test clipboard content"
	clipboardContent := oapi.ClipboardContent{
		Type: oapi.Text,
		Text: &text,
	}

	// Test GetClipboard200JSONResponse
	getResponse := oapi.GetClipboard200JSONResponse(clipboardContent)
	assert.Equal(t, oapi.Text, getResponse.Type)
	assert.Equal(t, &text, getResponse.Text)

	// Test SetClipboard200JSONResponse
	setResponse := oapi.SetClipboard200JSONResponse{
		Ok: true,
	}
	assert.True(t, setResponse.Ok)

	// Test SetClipboard400JSONResponse
	errorMsg := "error message"
	badRequestResponse := oapi.SetClipboard400JSONResponse{
		BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
			Message: errorMsg,
		},
	}
	assert.Equal(t, errorMsg, badRequestResponse.Message)
}

// Test ClipboardEvent struct
func TestClipboardEvent(t *testing.T) {
	preview := "Test preview"
	event := oapi.ClipboardEvent{
		Ts:      time.Now().Format(time.RFC3339),
		Type:    oapi.Text,
		Preview: &preview,
	}

	assert.NotEmpty(t, event.Ts)
	assert.Equal(t, oapi.Text, event.Type)
	assert.Equal(t, &preview, event.Preview)
}
