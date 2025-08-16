package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// clipboardManager handles clipboard operations
type clipboardManager struct {
	mu            sync.Mutex
	lastContent   string
	watchers      map[string]chan oapi.ClipboardEvent
	watchersMu    sync.RWMutex
	lastEventTime time.Time
}

// newClipboardManager creates a new clipboard manager
func newClipboardManager() *clipboardManager {
	cm := &clipboardManager{
		watchers:      make(map[string]chan oapi.ClipboardEvent),
		lastEventTime: time.Now(),
	}
	return cm
}

// getDisplay returns the DISPLAY environment variable or a default value
func getDisplay() string {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":20" // Default display for testing
	}
	return display
}

// getClipboardText retrieves text from the clipboard using xclip
func (cm *clipboardManager) getClipboardText(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)
	display := getDisplay()

	cmd := exec.CommandContext(ctx, "bash", "-lc", fmt.Sprintf("DISPLAY=%s xclip -selection clipboard -o 2>/dev/null || echo ''", display))
	out, err := cmd.Output()
	if err != nil {
		log.Error("failed to get clipboard content with xclip", "err", err)
		return "", fmt.Errorf("failed to get clipboard: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// setClipboardText sets text to the clipboard using xclip
func (cm *clipboardManager) setClipboardText(ctx context.Context, text string) error {
	log := logger.FromContext(ctx)
	display := getDisplay()

	// Create a temporary file to store the content (more reliable than piping for large content)
	cmd := exec.CommandContext(ctx, "bash", "-lc", fmt.Sprintf("printf %%s %s | DISPLAY=%s xclip -selection clipboard",
		escapeShellString(text), display))
	err := cmd.Run()
	if err != nil {
		log.Error("failed to set clipboard content with xclip", "err", err)
		return fmt.Errorf("failed to set clipboard: %w", err)
	}

	return nil
}

// escapeShellString escapes a string for use in shell commands
func escapeShellString(s string) string {
	return fmt.Sprintf("'%s'", strings.Replace(s, "'", "'\\''", -1))
}

// GetClipboard retrieves clipboard content
func (s *ApiService) GetClipboard(ctx context.Context, _ oapi.GetClipboardRequestObject) (oapi.GetClipboardResponseObject, error) {
	log := logger.FromContext(ctx)
	log.Info("getting clipboard content")

	// Initialize clipboard manager if not already done
	if s.clipboardManager == nil {
		s.clipboardManager = newClipboardManager()
	}

	text, err := s.clipboardManager.getClipboardText(ctx)
	if err != nil {
		log.Error("error getting clipboard content", "err", err)
		// Return empty text content on error
		return oapi.GetClipboard200JSONResponse{
			Type: oapi.Text,
			Text: new(string),
		}, nil
	}

	return oapi.GetClipboard200JSONResponse{
		Type: oapi.Text,
		Text: &text,
	}, nil
}

// SetClipboard sets clipboard content
func (s *ApiService) SetClipboard(ctx context.Context, req oapi.SetClipboardRequestObject) (oapi.SetClipboardResponseObject, error) {
	log := logger.FromContext(ctx)
	log.Info("setting clipboard content", "type", req.Body.Type)

	// Initialize clipboard manager if not already done
	if s.clipboardManager == nil {
		s.clipboardManager = newClipboardManager()
	}

	if req.Body.Type != oapi.Text {
		log.Error("unsupported clipboard content type", "type", req.Body.Type)
		return oapi.SetClipboard400JSONResponse{
			BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "only text clipboard content is supported",
			},
		}, nil
	}

	if req.Body.Text == nil {
		log.Error("text content is required for text type")
		return oapi.SetClipboard400JSONResponse{
			BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "text content is required for text type",
			},
		}, nil
	}

	err := s.clipboardManager.setClipboardText(ctx, *req.Body.Text)
	if err != nil {
		log.Error("failed to set clipboard content", "err", err)
		return oapi.SetClipboard500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: "failed to set clipboard content",
			},
		}, nil
	}

	// Update content for stream notifications
	s.clipboardManager.mu.Lock()
	oldContent := s.clipboardManager.lastContent
	s.clipboardManager.lastContent = *req.Body.Text
	s.clipboardManager.mu.Unlock()

	// If content changed, notify watchers
	if oldContent != *req.Body.Text {
		s.notifyClipboardChange(ctx, oapi.Text, *req.Body.Text)
	}

	return oapi.SetClipboard200JSONResponse{
		Ok: true,
	}, nil
}

// StreamClipboard streams clipboard changes as SSE events
func (s *ApiService) StreamClipboard(ctx context.Context, _ oapi.StreamClipboardRequestObject) (oapi.StreamClipboardResponseObject, error) {
	log := logger.FromContext(ctx)
	log.Info("starting clipboard stream")

	// Initialize clipboard manager if not already done
	if s.clipboardManager == nil {
		s.clipboardManager = newClipboardManager()
	}

	// Create a unique watcher ID
	watcherID := fmt.Sprintf("clipboard-%d", time.Now().UnixNano())

	// Create a channel for this watcher
	events := make(chan oapi.ClipboardEvent, 10)

	// Register the watcher
	s.clipboardManager.watchersMu.Lock()
	s.clipboardManager.watchers[watcherID] = events
	s.clipboardManager.watchersMu.Unlock()

	// Cleanup when the stream ends
	cleanup := func() {
		s.clipboardManager.watchersMu.Lock()
		delete(s.clipboardManager.watchers, watcherID)
		s.clipboardManager.watchersMu.Unlock()
		close(events)
		log.Info("clipboard stream ended", "watcher", watcherID)
	}

	// Start a goroutine to poll clipboard content
	go s.pollClipboard(ctx, watcherID)

	return &oapi.StreamClipboardResponseStream{
		Headers: oapi.StreamClipboard200ResponseHeaders{
			XSSEContentType: "application/json",
		},
		Events:  events,
		Cleanup: cleanup,
	}, nil
}

// pollClipboard periodically polls clipboard content and notifies watchers of changes
func (s *ApiService) pollClipboard(ctx context.Context, watcherID string) {
	log := logger.FromContext(ctx)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastText := ""

	for {
		select {
		case <-ctx.Done():
			log.Info("clipboard polling stopped due to context cancellation")
			return
		case <-ticker.C:
			text, err := s.clipboardManager.getClipboardText(ctx)
			if err != nil {
				log.Error("error polling clipboard", "err", err)
				continue
			}

			if text != lastText {
				log.Info("clipboard content changed")
				lastText = text
				s.notifyClipboardChange(ctx, oapi.Text, text)
			}
		}
	}
}

// notifyClipboardChange notifies all registered watchers of a clipboard change
func (s *ApiService) notifyClipboardChange(ctx context.Context, contentType oapi.ClipboardContentType, content string) {
	log := logger.FromContext(ctx)
	s.clipboardManager.mu.Lock()
	s.clipboardManager.lastContent = content
	s.clipboardManager.lastEventTime = time.Now()
	s.clipboardManager.mu.Unlock()

	// Create a preview of the content
	preview := content
	if len(preview) > 100 {
		preview = preview[:100]
	}

	// Create the event
	event := oapi.ClipboardEvent{
		Ts:      time.Now().Format(time.RFC3339),
		Type:    contentType,
		Preview: &preview,
	}

	// Get a copy of the watchers to avoid holding the lock while sending
	s.clipboardManager.watchersMu.RLock()
	watchers := make([]chan oapi.ClipboardEvent, 0, len(s.clipboardManager.watchers))
	for _, ch := range s.clipboardManager.watchers {
		watchers = append(watchers, ch)
	}
	s.clipboardManager.watchersMu.RUnlock()

	// Send event to all watchers
	for _, ch := range watchers {
		select {
		case ch <- event:
			// Sent successfully
		default:
			// Channel buffer is full, skip this event for this watcher
			log.Warn("skipped clipboard event for watcher with full buffer")
		}
	}

	// Log the event
	eventJSON, _ := json.Marshal(event)
	log.Info("sent clipboard change event", "event", string(eventJSON))
}
