package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/joho/godotenv"
	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
)

const (
	// S3 minimum part size (5MB) - except for the last part
	minPartSize = 5 * 1024 * 1024
)

// isTempFilename checks if the filename is a Chrome temp file
// Chrome creates files like .org.chromium.Chromium.XXXXXX during downloads
func isTempFilename(key string) bool {
	// Extract just the filename from the key (after last /)
	parts := strings.Split(key, "/")
	filename := parts[len(parts)-1]
	return strings.HasPrefix(filename, ".org.chromium.") ||
		strings.HasPrefix(filename, ".com.google.")
}

// S3Config holds S3/R2 configuration
type S3Config struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Region          string `json:"region"`
	Prefix          string `json:"prefix"` // Optional path prefix
}

// S3Client manages S3/R2 uploads
type S3Client struct {
	config   S3Config
	s3Client *s3.Client

	ctx    context.Context
	cancel context.CancelFunc

	// Track multipart uploads
	mu      sync.RWMutex
	uploads map[string]*multipartUpload

	state atomic.Int32

	// Metrics
	filesCreated atomic.Uint64
	filesUploaded atomic.Uint64
	bytesUploaded atomic.Uint64
	errors        atomic.Uint64
}

type multipartUpload struct {
	key          string
	finalKey     string // Desired final key after renames (updated by handleRename)
	uploadID     string
	parts        []types.CompletedPart
	buffer       bytes.Buffer
	partNum      int32
	started      bool // Whether multipart upload has been initiated
	hasData      bool // Whether any data has been written
	completed    bool // Whether upload has been completed (waiting for potential rename)
	renaming     bool // Whether a rename is currently in progress (prevents race conditions)
	closePending bool // FileClose received but waiting for rename before completing
}

// LoadS3ConfigFromEnv loads S3 config from .env file
func LoadS3ConfigFromEnv(envFile string) (S3Config, error) {
	if envFile == "" {
		envFile = ".env"
	}

	// Load .env file if it exists
	if _, err := os.Stat(envFile); err == nil {
		if err := godotenv.Load(envFile); err != nil {
			return S3Config{}, fmt.Errorf("load .env: %w", err)
		}
	}

	cfg := S3Config{
		Endpoint:        os.Getenv("S3_ENDPOINT"),
		Bucket:          os.Getenv("S3_BUCKET"),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		Region:          os.Getenv("S3_REGION"),
		Prefix:          os.Getenv("S3_PREFIX"),
	}

	if cfg.Region == "" {
		cfg.Region = "auto" // Default for R2
	}

	return cfg, cfg.Validate()
}

// ParseS3ConfigFromJSON parses S3 config from JSON string
func ParseS3ConfigFromJSON(jsonStr string) (S3Config, error) {
	var cfg S3Config
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		return cfg, fmt.Errorf("parse JSON: %w", err)
	}

	if cfg.Region == "" {
		cfg.Region = "auto"
	}

	return cfg, cfg.Validate()
}

// Validate checks required fields
func (c S3Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("S3_ENDPOINT is required")
	}
	if c.Bucket == "" {
		return errors.New("S3_BUCKET is required")
	}
	if c.AccessKeyID == "" {
		return errors.New("S3_ACCESS_KEY_ID is required")
	}
	if c.SecretAccessKey == "" {
		return errors.New("S3_SECRET_ACCESS_KEY is required")
	}
	return nil
}

// NewS3Client creates a new S3/R2 transport client
func NewS3Client(cfg S3Config) (*S3Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create S3 client
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true // Required for R2 and most S3-compatible storage
	})

	c := &S3Client{
		config:   cfg,
		s3Client: s3Client,
		ctx:      ctx,
		cancel:   cancel,
		uploads:  make(map[string]*multipartUpload),
	}

	c.state.Store(int32(StateConnected))
	return c, nil
}

// Connect is a no-op for S3 (already connected on creation)
func (c *S3Client) Connect() error {
	logging.Info("S3 client ready for bucket: %s", c.config.Bucket)
	return nil
}

// Send handles file operations
func (c *S3Client) Send(msgType byte, payload interface{}) error {
	return c.sendInternal(msgType, payload)
}

// SendSync sends a message synchronously (same as Send for S3 since it's already synchronous)
func (c *S3Client) SendSync(msgType byte, payload interface{}) error {
	return c.sendInternal(msgType, payload)
}

// sendInternal handles the actual file operations
func (c *S3Client) sendInternal(msgType byte, payload interface{}) error {
	switch msgType {
	case protocol.MsgFileCreate:
		msg := payload.(*protocol.FileCreate)
		return c.handleFileCreate(msg)

	case protocol.MsgWriteChunk:
		msg := payload.(*protocol.WriteChunk)
		return c.handleWriteChunk(msg)

	case protocol.MsgFileClose:
		msg := payload.(*protocol.FileClose)
		return c.handleFileClose(msg)

	case protocol.MsgRename:
		msg := payload.(*protocol.Rename)
		return c.handleRename(msg)

	case protocol.MsgDelete:
		msg := payload.(*protocol.Delete)
		return c.handleDelete(msg)

	case protocol.MsgTruncate:
		// S3 doesn't support truncate, log warning
		logging.Warn("Truncate not supported for S3")
		return nil

	default:
		return fmt.Errorf("unknown message type: 0x%02x", msgType)
	}
}

// SendAndReceive sends and returns appropriate ACK for S3
func (c *S3Client) SendAndReceive(msgType byte, payload interface{}) (byte, []byte, error) {
	// For S3, we send and return a fake ACK
	err := c.Send(msgType, payload)

	// Return appropriate ACK based on message type
	switch msgType {
	case protocol.MsgFileCreate:
		msg := payload.(*protocol.FileCreate)
		ack := protocol.FileCreateAck{
			FileID:  msg.FileID,
			Success: err == nil,
		}
		if err != nil {
			ack.Error = err.Error()
		}
		data, _ := json.Marshal(ack)
		return protocol.MsgFileCreateAck, data, nil

	case protocol.MsgWriteChunk:
		if err != nil {
			return 0, nil, err
		}
		msg := payload.(*protocol.WriteChunk)
		ack := protocol.WriteAck{
			FileID:  msg.FileID,
			Offset:  msg.Offset,
			Written: len(msg.Data),
		}
		data, _ := json.Marshal(ack)
		return protocol.MsgWriteAck, data, nil
	}

	if err != nil {
		return 0, nil, err
	}
	return 0, nil, nil
}

func (c *S3Client) handleFileCreate(msg *protocol.FileCreate) error {
	key := c.config.Prefix + msg.Filename

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we already have an upload for this FileID
	// Don't overwrite if one exists (preserves finalKey from renames)
	if existing, ok := c.uploads[msg.FileID]; ok {
		logging.Info("S3: FileCreate for existing id=%s, keeping finalKey=%s (new filename=%s)", msg.FileID, existing.finalKey, key)
		return nil
	}

	// Don't start multipart upload yet - wait for first write
	// This handles Chrome's placeholder files that get created and immediately closed
	c.uploads[msg.FileID] = &multipartUpload{
		key:      key,
		finalKey: key, // Will be updated by handleRename if rename arrives before upload completes
		parts:    make([]types.CompletedPart, 0),
		partNum:  0,
		started:  false,
		hasData:  false,
	}

	c.filesCreated.Add(1)
	logging.Info("S3: Registered file key=%s id=%s", key, msg.FileID)
	return nil
}

func (c *S3Client) handleWriteChunk(msg *protocol.WriteChunk) error {
	c.mu.Lock()
	upload, ok := c.uploads[msg.FileID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("unknown file ID: %s", msg.FileID)
	}

	// Start multipart upload on first write (lazy initialization)
	// Use finalKey which may have been updated by rename before writes started
	if !upload.started {
		// Use finalKey - it may have been updated by rename(s) before first write
		uploadKey := upload.finalKey
		logging.Info("S3: Starting multipart upload for id=%s with key=%s (original key was %s)", msg.FileID, uploadKey, upload.key)
		output, err := c.s3Client.CreateMultipartUpload(c.ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(c.config.Bucket),
			Key:    aws.String(uploadKey),
		})
		if err != nil {
			c.mu.Unlock()
			c.errors.Add(1)
			return fmt.Errorf("create multipart upload: %w", err)
		}
		// Update key to match what we actually used for the multipart upload
		upload.key = uploadKey
		upload.uploadID = *output.UploadId
		upload.started = true
		logging.Info("S3: Started multipart upload for %s", uploadKey)
	}

	// Buffer the data
	upload.buffer.Write(msg.Data)
	upload.hasData = true
	c.bytesUploaded.Add(uint64(len(msg.Data)))

	// If buffer >= 5MB, upload a part
	if upload.buffer.Len() >= minPartSize {
		if err := c.uploadPartLocked(upload); err != nil {
			c.mu.Unlock()
			return err
		}
	}

	c.mu.Unlock()
	return nil
}

func (c *S3Client) uploadPartLocked(upload *multipartUpload) error {
	upload.partNum++
	data := upload.buffer.Bytes()
	upload.buffer.Reset()

	output, err := c.s3Client.UploadPart(c.ctx, &s3.UploadPartInput{
		Bucket:     aws.String(c.config.Bucket),
		Key:        aws.String(upload.key),
		UploadId:   aws.String(upload.uploadID),
		PartNumber: aws.Int32(upload.partNum),
		Body:       bytes.NewReader(data),
	})
	if err != nil {
		c.errors.Add(1)
		return fmt.Errorf("upload part %d: %w", upload.partNum, err)
	}

	upload.parts = append(upload.parts, types.CompletedPart{
		ETag:       output.ETag,
		PartNumber: aws.Int32(upload.partNum),
	})

	logging.Debug("S3: Uploaded part %d (%d bytes) for %s", upload.partNum, len(data), upload.key)
	return nil
}

func (c *S3Client) handleFileClose(msg *protocol.FileClose) error {
	c.mu.Lock()
	upload, ok := c.uploads[msg.FileID]
	if !ok {
		c.mu.Unlock()
		logging.Info("S3: FileClose for unknown ID %s", msg.FileID)
		return nil
	}

	logging.Info("S3: FileClose id=%s key=%s finalKey=%s started=%v hasData=%v completed=%v closePending=%v", msg.FileID, upload.key, upload.finalKey, upload.started, upload.hasData, upload.completed, upload.closePending)

	// If already completed, this is a duplicate close - ignore
	if upload.completed {
		c.mu.Unlock()
		logging.Debug("S3: FileClose for already completed upload %s", upload.key)
		return nil
	}

	// If no data was ever written, this is likely a placeholder file from Chrome's
	// open-close-open pattern. DON'T delete from map - writes may come later!
	if !upload.hasData {
		c.mu.Unlock()
		logging.Debug("S3: FileClose with no data for %s - keeping registration for late writes", upload.key)
		return nil
	}

	// If multipart upload was never started (shouldn't happen if hasData is true, but be safe)
	if !upload.started {
		c.mu.Unlock()
		logging.Debug("S3: FileClose but multipart never started for %s", upload.key)
		return nil
	}

	// Chrome sends FileClose BEFORE all data is written when file still has temp name!
	// Don't complete the upload until we've seen a rename (temp -> .crdownload)
	// Check if file still has temp name AND no rename has happened yet
	if isTempFilename(upload.finalKey) {
		upload.closePending = true
		c.mu.Unlock()
		logging.Info("S3: FileClose with temp filename - deferring completion until rename (closePending=true)")
		return nil
	}

	// File has been renamed (or was never a temp file) - proceed with completion
	c.mu.Unlock()
	return c.completeUpload(msg.FileID, upload)
}

// completeUpload finalizes the multipart upload to S3
func (c *S3Client) completeUpload(fileID string, upload *multipartUpload) error {
	c.mu.Lock()
	uploadKey := upload.key

	// Upload remaining data as final part
	if upload.buffer.Len() > 0 {
		if err := c.uploadPartLocked(upload); err != nil {
			c.mu.Unlock()
			// Abort the upload
			c.s3Client.AbortMultipartUpload(c.ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(c.config.Bucket),
				Key:      aws.String(uploadKey),
				UploadId: aws.String(upload.uploadID),
			})
			return err
		}
	}
	c.mu.Unlock()

	// Complete the multipart upload
	_, err := c.s3Client.CompleteMultipartUpload(c.ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(c.config.Bucket),
		Key:      aws.String(uploadKey),
		UploadId: aws.String(upload.uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: upload.parts,
		},
	})
	if err != nil {
		c.errors.Add(1)
		// Abort on error
		c.s3Client.AbortMultipartUpload(c.ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(c.config.Bucket),
			Key:      aws.String(uploadKey),
			UploadId: aws.String(upload.uploadID),
		})
		return fmt.Errorf("complete multipart upload: %w", err)
	}

	logging.Info("S3: Completed upload for %s (%d parts)", uploadKey, len(upload.parts))

	// Check if finalKey was updated by a rename that arrived during CompleteMultipartUpload
	c.mu.Lock()
	currentFinalKey := upload.finalKey
	upload.completed = true
	upload.key = uploadKey

	if currentFinalKey != uploadKey {
		upload.renaming = true
		c.mu.Unlock()
		logging.Info("S3: Rename arrived during upload completion, applying: %s -> %s", uploadKey, currentFinalKey)
		if err := c.doS3RenameLoop(uploadKey, fileID); err != nil {
			logging.Error("S3: Failed to apply deferred rename: %v", err)
		}
	} else {
		c.mu.Unlock()
	}

	c.filesUploaded.Add(1)
	return nil
}

func (c *S3Client) handleRename(msg *protocol.Rename) error {
	newKey := c.config.Prefix + msg.NewName
	oldKey := c.config.Prefix + msg.OldName

	logging.Info("S3: Rename called: id=%s old=%s new=%s", msg.FileID, msg.OldName, msg.NewName)

	// First check if there's an active/completed upload for this file by FileID
	c.mu.Lock()
	if msg.FileID != "" {
		if upload, ok := c.uploads[msg.FileID]; ok {
			// Always update finalKey to the desired destination
			upload.finalKey = newKey

			// If closePending was set, clear it - the next FileClose after rename will trigger completion
			// DON'T complete here because Chrome may write more data after rename!
			if upload.closePending {
				upload.closePending = false
				logging.Info("S3: Rename cleared closePending, will complete on next FileClose: %s -> %s (id=%s)", upload.key, newKey, msg.FileID)
			}

			if upload.completed {
				// Upload already completed in S3
				if upload.renaming {
					// A rename is already in progress - just update finalKey (done above)
					c.mu.Unlock()
					logging.Info("S3: Rename queued (another rename in progress): -> %s (id=%s)", newKey, msg.FileID)
					return nil
				}
				// Start the rename - use upload.key (actual S3 location)
				actualKey := upload.key
				upload.renaming = true
				c.mu.Unlock()
				logging.Info("S3: Rename after completion: %s -> %s (id=%s)", actualKey, newKey, msg.FileID)
				return c.doS3RenameLoop(actualKey, msg.FileID)
			} else if upload.started {
				c.mu.Unlock()
				logging.Info("S3: Rename during upload (will apply at close): %s -> %s (id=%s)", upload.key, newKey, msg.FileID)
			} else {
				c.mu.Unlock()
				logging.Info("S3: Rename before upload start: finalKey updated to %s (id=%s)", newKey, msg.FileID)
			}
			return nil
		}
		logging.Info("S3: Rename FileID=%s not found in uploads map", msg.FileID)
	}

	// Fallback: search by old filename if FileID lookup failed
	logging.Info("S3: Rename fallback search by oldKey=%s", oldKey)
	for fileID, upload := range c.uploads {
		logging.Info("S3: Rename checking upload id=%s key=%s finalKey=%s completed=%v", fileID, upload.key, upload.finalKey, upload.completed)
		if upload.key == oldKey || upload.finalKey == oldKey {
			upload.finalKey = newKey

			if upload.completed {
				if upload.renaming {
					c.mu.Unlock()
					logging.Info("S3: Rename queued by filename (another rename in progress): -> %s (id=%s)", newKey, fileID)
					return nil
				}
				actualKey := upload.key
				upload.renaming = true
				c.mu.Unlock()
				logging.Info("S3: Rename by filename after completion: %s -> %s (id=%s)", actualKey, newKey, fileID)
				return c.doS3RenameLoop(actualKey, fileID)
			} else if upload.started {
				c.mu.Unlock()
				logging.Info("S3: Rename by filename during upload: %s -> %s (id=%s)", upload.key, newKey, fileID)
			} else {
				c.mu.Unlock()
				logging.Info("S3: Rename by filename before upload start: -> %s (id=%s)", newKey, fileID)
			}
			return nil
		}
	}
	c.mu.Unlock()

	// No active upload found - check if object exists in S3 and rename it
	logging.Info("S3: Rename - no active upload found for id=%s or key=%s, trying S3 directly", msg.FileID, oldKey)

	_, err := c.s3Client.HeadObject(c.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		// Source doesn't exist - this is expected for Chrome's placeholder files
		// Just log and return success (the rename is a no-op)
		logging.Debug("S3: Rename skipped - source %s does not exist (placeholder file)", oldKey)
		return nil
	}

	// S3 doesn't support rename, so we copy + delete
	_, err = c.s3Client.CopyObject(c.ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.config.Bucket),
		CopySource: aws.String(c.config.Bucket + "/" + oldKey),
		Key:        aws.String(newKey),
	})
	if err != nil {
		c.errors.Add(1)
		return fmt.Errorf("copy object: %w", err)
	}

	_, err = c.s3Client.DeleteObject(c.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		logging.Warn("S3: Failed to delete old key after rename: %v", err)
		// Don't return error - copy succeeded
	}

	logging.Debug("S3: Renamed %s -> %s", oldKey, newKey)
	return nil
}

// doS3RenameLoop performs S3 renames in a loop until key == finalKey
// This handles multiple renames that arrive while a rename is in progress
func (c *S3Client) doS3RenameLoop(currentKey string, fileID string) error {
	for {
		// Get the target key under lock
		c.mu.Lock()
		upload, ok := c.uploads[fileID]
		if !ok {
			c.mu.Unlock()
			return nil
		}
		targetKey := upload.finalKey
		if currentKey == targetKey {
			// No more renames needed
			upload.renaming = false
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()

		// Do the S3 copy+delete
		logging.Info("S3: Renaming %s -> %s", currentKey, targetKey)
		_, err := c.s3Client.CopyObject(c.ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(c.config.Bucket),
			CopySource: aws.String(c.config.Bucket + "/" + currentKey),
			Key:        aws.String(targetKey),
		})
		if err != nil {
			c.errors.Add(1)
			logging.Error("S3: Failed to copy %s -> %s: %v", currentKey, targetKey, err)
			// Clear renaming flag on error
			c.mu.Lock()
			if upload, ok := c.uploads[fileID]; ok {
				upload.renaming = false
			}
			c.mu.Unlock()
			return fmt.Errorf("copy object: %w", err)
		}

		_, err = c.s3Client.DeleteObject(c.ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(c.config.Bucket),
			Key:    aws.String(currentKey),
		})
		if err != nil {
			logging.Warn("S3: Failed to delete old key %s after rename: %v", currentKey, err)
		}

		// Update the key and check if we need another rename
		c.mu.Lock()
		if upload, ok := c.uploads[fileID]; ok {
			upload.key = targetKey
		}
		c.mu.Unlock()

		logging.Info("S3: Renamed %s -> %s", currentKey, targetKey)
		currentKey = targetKey
		// Loop will check if finalKey changed during this rename
	}
}

// doS3Rename performs a single S3 copy+delete (used by handleFileClose)
func (c *S3Client) doS3Rename(oldKey, newKey, fileID string) error {
	_, err := c.s3Client.CopyObject(c.ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.config.Bucket),
		CopySource: aws.String(c.config.Bucket + "/" + oldKey),
		Key:        aws.String(newKey),
	})
	if err != nil {
		c.errors.Add(1)
		logging.Error("S3: Failed to copy %s -> %s: %v", oldKey, newKey, err)
		return fmt.Errorf("copy object: %w", err)
	}

	_, err = c.s3Client.DeleteObject(c.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		logging.Warn("S3: Failed to delete old key %s after rename: %v", oldKey, err)
	}

	// Update the upload entry's key to reflect the new name
	c.mu.Lock()
	if upload, ok := c.uploads[fileID]; ok {
		upload.key = newKey
	}
	c.mu.Unlock()

	logging.Info("S3: Renamed %s -> %s", oldKey, newKey)
	return nil
}

func (c *S3Client) handleDelete(msg *protocol.Delete) error {
	key := c.config.Prefix + msg.Filename

	_, err := c.s3Client.DeleteObject(c.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		c.errors.Add(1)
		return fmt.Errorf("delete object: %w", err)
	}

	logging.Debug("S3: Deleted %s", key)
	return nil
}

// State returns current state (always connected for S3)
func (c *S3Client) State() ConnectionState {
	return ConnectionState(c.state.Load())
}

// Stats returns client statistics
func (c *S3Client) Stats() map[string]uint64 {
	return map[string]uint64{
		"files_created":  c.filesCreated.Load(),
		"files_uploaded": c.filesUploaded.Load(),
		"bytes_uploaded": c.bytesUploaded.Load(),
		"errors":         c.errors.Load(),
	}
}

// Close cleans up resources
func (c *S3Client) Close() error {
	c.cancel()

	// Abort any pending uploads that were actually started
	c.mu.Lock()
	defer c.mu.Unlock()

	for fileID, upload := range c.uploads {
		if upload.started && upload.uploadID != "" {
			logging.Warn("S3: Aborting incomplete upload for %s", upload.key)
			c.s3Client.AbortMultipartUpload(c.ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(c.config.Bucket),
				Key:      aws.String(upload.key),
				UploadId: aws.String(upload.uploadID),
			})
		}
		delete(c.uploads, fileID)
	}

	return nil
}

// Compile-time interface check
var _ Transport = (*S3Client)(nil)
