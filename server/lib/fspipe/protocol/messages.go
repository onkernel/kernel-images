package protocol

// Message types
const (
	MsgFileCreate    byte = 0x01
	MsgFileCreateAck byte = 0x02 // Response to FileCreate
	MsgFileClose     byte = 0x03
	MsgWriteChunk    byte = 0x10
	MsgWriteAck      byte = 0x11
	MsgTruncate      byte = 0x12
	MsgRename        byte = 0x20
	MsgDelete        byte = 0x21
)

// ChunkSize is the default chunk size for file writes (64KB)
const ChunkSize = 64 * 1024

// FileCreate is sent when a new file is created
type FileCreate struct {
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
	Mode     uint32 `json:"mode"`
}

// FileCreateAck is the response to FileCreate
type FileCreateAck struct {
	FileID  string `json:"file_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// FileClose is sent when a file handle is closed
type FileClose struct {
	FileID string `json:"file_id"`
}

// WriteChunk is sent for each chunk of file data
type WriteChunk struct {
	FileID string `json:"file_id"`
	Offset int64  `json:"offset"`
	Data   []byte `json:"data"`
}

// WriteAck is sent as acknowledgment for a write
type WriteAck struct {
	FileID  string `json:"file_id"`
	Offset  int64  `json:"offset"`
	Written int    `json:"written"`
	Error   string `json:"error,omitempty"`
}

// Truncate is sent to truncate a file
type Truncate struct {
	FileID string `json:"file_id"`
	Size   int64  `json:"size"`
}

// Rename is sent to rename a file
type Rename struct {
	FileID  string `json:"file_id"`
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

// Delete is sent to delete a file
type Delete struct {
	Filename string `json:"filename"`
}

// Message wraps any protocol message with its type
type Message struct {
	Type    byte
	Payload interface{}
}
