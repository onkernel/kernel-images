package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Frame format: [Length: 4 bytes (uint32 BE)] [Type: 1 byte] [Payload: N bytes JSON]

// Encoder writes framed messages to a writer
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new encoder
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes a message with length-prefix framing
func (e *Encoder) Encode(msgType byte, payload interface{}) error {
	// Marshal payload to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Calculate total frame length (type byte + payload)
	frameLen := uint32(1 + len(data))

	// Write length prefix (4 bytes, big-endian)
	if err := binary.Write(e.w, binary.BigEndian, frameLen); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	// Write message type (1 byte)
	if _, err := e.w.Write([]byte{msgType}); err != nil {
		return fmt.Errorf("write type: %w", err)
	}

	// Write payload
	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// Decoder reads framed messages from a reader
type Decoder struct {
	r io.Reader
}

// NewDecoder creates a new decoder
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads a framed message and returns the type and raw JSON payload
func (d *Decoder) Decode() (byte, []byte, error) {
	// Read length prefix (4 bytes)
	var frameLen uint32
	if err := binary.Read(d.r, binary.BigEndian, &frameLen); err != nil {
		return 0, nil, err
	}

	if frameLen < 1 {
		return 0, nil, fmt.Errorf("invalid frame length: %d", frameLen)
	}

	// Read type byte
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(d.r, typeBuf); err != nil {
		return 0, nil, fmt.Errorf("read type: %w", err)
	}

	// Read payload
	payloadLen := frameLen - 1
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(d.r, payload); err != nil {
			return 0, nil, fmt.Errorf("read payload: %w", err)
		}
	}

	return typeBuf[0], payload, nil
}

// DecodePayload unmarshals JSON payload into the target struct
func DecodePayload(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
