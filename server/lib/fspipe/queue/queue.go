package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
)

var (
	ErrQueueFull    = errors.New("queue is full")
	ErrQueueClosed  = errors.New("queue is closed")
	ErrSendTimeout  = errors.New("send timeout")
	ErrAckTimeout   = errors.New("acknowledgment timeout")
)

// Message represents a queued message with tracking info
type Message struct {
	ID        uint64
	Type      byte
	Payload   interface{}
	Result    chan error
	Timestamp time.Time
	Retries   int
}

// Queue is a bounded message queue with backpressure
type Queue struct {
	messages chan *Message
	pending  sync.Map // msgID -> *Message (waiting for ACK)

	seqNum    uint64
	maxSize   int
	closed    atomic.Bool

	ackTimeout time.Duration
	maxRetries int

	mu sync.Mutex
}

// Config holds queue configuration
type Config struct {
	MaxSize    int           // Maximum queue size
	AckTimeout time.Duration // Timeout waiting for ACK
	MaxRetries int           // Maximum send retries
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxSize:    1000,
		AckTimeout: 30 * time.Second,
		MaxRetries: 3,
	}
}

// New creates a new message queue
func New(cfg Config) *Queue {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 1000
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return &Queue{
		messages:   make(chan *Message, cfg.MaxSize),
		maxSize:    cfg.MaxSize,
		ackTimeout: cfg.AckTimeout,
		maxRetries: cfg.MaxRetries,
	}
}

// Enqueue adds a message to the queue
// Returns immediately if queue has space, blocks with timeout otherwise
func (q *Queue) Enqueue(ctx context.Context, msgType byte, payload interface{}) (*Message, error) {
	if q.closed.Load() {
		return nil, ErrQueueClosed
	}

	msg := &Message{
		ID:        atomic.AddUint64(&q.seqNum, 1),
		Type:      msgType,
		Payload:   payload,
		Result:    make(chan error, 1),
		Timestamp: time.Now(),
	}

	select {
	case q.messages <- msg:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Queue full - try with timeout
		select {
		case q.messages <- msg:
			return msg, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, ErrQueueFull
		}
	}
}

// EnqueueSync adds a message and waits for the result
func (q *Queue) EnqueueSync(ctx context.Context, msgType byte, payload interface{}) error {
	msg, err := q.Enqueue(ctx, msgType, payload)
	if err != nil {
		return err
	}

	// Wait for send completion with timeout
	select {
	case err := <-msg.Result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(q.ackTimeout):
		return ErrSendTimeout
	}
}

// Dequeue removes the next message from the queue
func (q *Queue) Dequeue(ctx context.Context) (*Message, error) {
	select {
	case msg, ok := <-q.messages:
		if !ok {
			return nil, ErrQueueClosed
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TrackPending marks a message as pending ACK
func (q *Queue) TrackPending(msg *Message) {
	q.pending.Store(msg.ID, msg)
}

// AckMessage marks a message as successfully sent
func (q *Queue) AckMessage(msgID uint64, err error) {
	if val, ok := q.pending.LoadAndDelete(msgID); ok {
		msg := val.(*Message)
		select {
		case msg.Result <- err:
		default:
			logging.Warn("Message %d result channel full", msgID)
		}
	}
}

// GetPendingCount returns the number of pending messages
func (q *Queue) GetPendingCount() int {
	count := 0
	q.pending.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// RetryPending re-queues all pending messages for retry
func (q *Queue) RetryPending() int {
	count := 0
	q.pending.Range(func(key, val interface{}) bool {
		msg := val.(*Message)
		msg.Retries++

		if msg.Retries > q.maxRetries {
			q.pending.Delete(key)
			select {
			case msg.Result <- errors.New("max retries exceeded"):
			default:
			}
			logging.Warn("Message %d exceeded max retries", msg.ID)
		} else {
			// Re-queue for retry
			select {
			case q.messages <- msg:
				count++
			default:
				logging.Error("Cannot re-queue message %d: queue full", msg.ID)
			}
		}
		q.pending.Delete(key)
		return true
	})
	return count
}

// Len returns the current queue length
func (q *Queue) Len() int {
	return len(q.messages)
}

// Close closes the queue
func (q *Queue) Close() {
	if q.closed.CompareAndSwap(false, true) {
		close(q.messages)

		// Fail all pending messages
		q.pending.Range(func(key, val interface{}) bool {
			msg := val.(*Message)
			select {
			case msg.Result <- ErrQueueClosed:
			default:
			}
			q.pending.Delete(key)
			return true
		})
	}
}
