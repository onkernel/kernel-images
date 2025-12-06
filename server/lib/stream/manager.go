package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

type StreamManager struct {
	mu      sync.Mutex
	streams map[string]Streamer
}

func NewStreamManager() *StreamManager {
	return &StreamManager{
		streams: make(map[string]Streamer),
	}
}

func (sm *StreamManager) GetStream(id string) (Streamer, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stream, ok := sm.streams[id]
	return stream, ok
}

func (sm *StreamManager) ListStreams(ctx context.Context) []Streamer {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	streams := make([]Streamer, 0, len(sm.streams))
	for _, stream := range sm.streams {
		streams = append(streams, stream)
	}
	return streams
}

func (sm *StreamManager) RegisterStream(ctx context.Context, streamer Streamer) error {
	log := logger.FromContext(ctx)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.streams[streamer.ID()]; exists {
		return fmt.Errorf("stream with id '%s' already exists", streamer.ID())
	}

	sm.streams[streamer.ID()] = streamer
	log.Info("registered new stream", "id", streamer.ID())
	return nil
}

func (sm *StreamManager) DeregisterStream(ctx context.Context, streamer Streamer) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.streams, streamer.ID())
	return nil
}

func (sm *StreamManager) StopAll(ctx context.Context) error {
	log := logger.FromContext(ctx)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	var errs []error
	for id, streamer := range sm.streams {
		if streamer.IsStreaming(ctx) {
			if err := streamer.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("failed to stop stream '%s': %w", id, err))
				log.Error("failed to stop stream during shutdown", "id", id, "err", err)
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
