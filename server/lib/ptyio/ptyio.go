// Package ptyio provides shared utilities for PTY I/O operations and WebSocket attach protocol types.
package ptyio

import (
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// PTY reading constants
const (
	// PollTimeoutMs is the timeout in milliseconds for polling PTY readability.
	// Short enough to respond quickly to shutdown signals, long enough to avoid busy-waiting.
	PollTimeoutMs = 100

	// MaxTerminalDimension is the maximum allowed value for terminal rows/cols.
	MaxTerminalDimension = 65535
)

// AttachMessageType represents the type of control message in the WebSocket attach protocol.
type AttachMessageType string

const (
	// AttachMessageResize is sent by clients to resize the PTY.
	AttachMessageResize AttachMessageType = "resize"
	// AttachMessageExit is sent by the server when the process exits.
	AttachMessageExit AttachMessageType = "exit"
	// AttachMessageError is sent by the server on errors.
	AttachMessageError AttachMessageType = "error"
)

// AttachControlMessage represents control messages sent over the WebSocket attach connection.
// Control messages use TextMessage type, while data uses BinaryMessage.
type AttachControlMessage struct {
	Type     AttachMessageType `json:"type"`               // "resize", "exit", "error"
	Rows     int               `json:"rows,omitempty"`     // For resize
	Cols     int               `json:"cols,omitempty"`     // For resize
	ExitCode *int              `json:"exitCode,omitempty"` // For exit
	Message  string            `json:"message,omitempty"`  // For error
}

// DataWriter is a function that writes data read from the PTY.
// Returns an error if the write fails or shutdown is requested.
type DataWriter func(data []byte) error

// ReadPTYToWriter reads from a PTY file and writes data using the provided writer function.
// It uses non-blocking polling to allow checking the stop channel periodically.
// Returns when the PTY is closed, an error occurs, or stop is signaled.
func ReadPTYToWriter(ptyFile *os.File, writer DataWriter, stop <-chan struct{}) error {
	fd := int(ptyFile.Fd())
	buf := make([]byte, 32*1024)

	for {
		// Check for stop first to avoid extra reads after shutdown.
		select {
		case <-stop:
			return nil
		default:
		}

		pfds := []unix.PollFd{
			{Fd: int32(fd), Events: unix.POLLIN},
		}
		_, perr := unix.Poll(pfds, PollTimeoutMs)
		if perr != nil && perr != syscall.EINTR {
			return perr
		}

		// If not readable, loop around and re-check stop.
		if pfds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
			continue
		}

		n, rerr := ptyFile.Read(buf)
		if n > 0 {
			// Copy data to avoid race with next read
			data := make([]byte, n)
			copy(data, buf[:n])
			if err := writer(data); err != nil {
				return err
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			if errno, ok := rerr.(syscall.Errno); ok {
				// EIO is observed on PTY when the slave closes; treat as EOF.
				if errno == syscall.EIO {
					return nil
				}
				// Spurious would-block after poll; just continue.
				if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK {
					continue
				}
			}
			return rerr
		}
	}
}
