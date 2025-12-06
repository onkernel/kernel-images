package stream

import (
	"context"
	"time"
)

type Mode string

const (
	ModeInternal Mode = "internal"
	ModeRemote   Mode = "remote"
)

// Params holds stream creation settings.
type Params struct {
	FrameRate         *int
	DisplayNum        *int
	IngestURL         string
	Mode              Mode
	PlaybackURL       *string
	SecurePlaybackURL *string
}

// Metadata describes a running stream.
type Metadata struct {
	ID                string
	Mode              Mode
	IngestURL         string
	PlaybackURL       *string
	SecurePlaybackURL *string
	StartedAt         time.Time
}

// Streamer defines the interface for a streaming session.
type Streamer interface {
	ID() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsStreaming(ctx context.Context) bool
	Metadata() Metadata
}

// Manager defines the interface for tracking streaming sessions.
type Manager interface {
	GetStream(id string) (Streamer, bool)
	ListStreams(ctx context.Context) []Streamer
	RegisterStream(ctx context.Context, streamer Streamer) error
	DeregisterStream(ctx context.Context, streamer Streamer) error
	StopAll(ctx context.Context) error
}

// FFmpegStreamerFactory returns a Streamer configured with the provided id and params.
type FFmpegStreamerFactory func(id string, params Params) (Streamer, error)

// InternalServer represents the internal RTMP(S) server used for live streaming.
type InternalServer interface {
	Start(ctx context.Context) error
	EnsureStream(path string)
	IngestURL(streamPath string) string
	PlaybackURLs(host string, streamPath string) (rtmpURL *string, rtmpsURL *string)
	Close(ctx context.Context) error
}
