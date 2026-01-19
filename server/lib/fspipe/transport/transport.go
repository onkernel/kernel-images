package transport

import (
	"fmt"
	"net/url"
)

// Transport defines the interface for all transport implementations
type Transport interface {
	// Connect establishes connection to the remote server
	Connect() error

	// Send sends a message asynchronously (non-blocking)
	Send(msgType byte, payload interface{}) error

	// SendSync sends a message and waits for send completion (blocking)
	// This ensures the message is actually sent before returning, unlike Send which queues.
	// Use this for messages that must be delivered before subsequent operations.
	SendSync(msgType byte, payload interface{}) error

	// SendAndReceive sends a message and waits for a response
	SendAndReceive(msgType byte, payload interface{}) (byte, []byte, error)

	// State returns the current connection state
	State() ConnectionState

	// Stats returns transport statistics
	Stats() map[string]uint64

	// Close closes the transport
	Close() error
}

// NewTransport creates a transport based on the URL scheme
func NewTransport(remoteURL string, config ClientConfig) (Transport, error) {
	u, err := url.Parse(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	switch u.Scheme {
	case "tcp":
		return NewClientWithConfig(u.Host, config), nil
	case "ws", "wss":
		return NewWebSocketClient(remoteURL, config), nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s (use tcp://, ws://, or wss://)", u.Scheme)
	}
}

// Compile-time interface checks
var _ Transport = (*Client)(nil)
var _ Transport = (*WebSocketClient)(nil)
