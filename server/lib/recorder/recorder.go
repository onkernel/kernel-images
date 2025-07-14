package recorder

import (
	"context"
	"io"
	"time"
)

// Recorder defines the interface for recording functionality.
type Recorder interface {
	ID() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	ForceStop(ctx context.Context) error
	IsRecording(ctx context.Context) bool
	Recording(ctx context.Context) (io.ReadCloser, *RecordingMetadata, error) // Returns the recording file as a ReadCloser
}

type RecordingMetadata struct {
	Size      int64
	StartTime time.Time
	EndTime   time.Time
}

// RecordManager defines the interface for managing multiple recorder instances.
// Implementations should be thread-safe for concurrent access.
type RecordManager interface {
	// GetRecorder retrieves a recorder by its ID.
	// Returns the recorder and true if found, nil and false otherwise.
	GetRecorder(id string) (Recorder, bool)

	// ListActiveRecorders returns a list of IDs for all registered recorders
	ListActiveRecorders(ctx context.Context) []Recorder

	// DeregisterRecorder removes a recorder from the manager.
	DeregisterRecorder(ctx context.Context, recorder Recorder) error

	// RegisterRecorder registers a recorder with the given ID.
	// Returns an error if a recorder with the same ID already exists.
	RegisterRecorder(ctx context.Context, recorder Recorder) error

	// StopAll stops all active recorders.
	StopAll(ctx context.Context) error
}
