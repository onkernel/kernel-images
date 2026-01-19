package api

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/onkernel/kernel-images/server/lib/fspipe/daemon"
	"github.com/onkernel/kernel-images/server/lib/fspipe/health"
	"github.com/onkernel/kernel-images/server/lib/fspipe/listener"
	"github.com/onkernel/kernel-images/server/lib/fspipe/transport"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/policy"
)

const (
	defaultFspipeMountPath    = "/home/kernel/fspipe-downloads"
	defaultFspipeHealthPort   = 8090
	defaultFspipeListenerPort = 9000
	defaultFspipeOutputDir    = "/tmp/fspipe-output"
)

// fspipeState holds the state of the running fspipe daemon
type fspipeState struct {
	mu sync.RWMutex

	running       bool
	transportMode string // "websocket" or "s3"
	mountPath     string
	wsEndpoint    string
	s3Bucket      string
	healthPort    int
	listenerPort  int
	outputDir     string

	transport      transport.Transport
	fuseServer     *fuse.Server
	healthServer   *health.Server
	listenerServer *listener.Server
}

var fspipe = &fspipeState{}

// StartFspipe starts the fspipe daemon with the given configuration
func (s *ApiService) StartFspipe(ctx context.Context, req oapi.StartFspipeRequestObject) (oapi.StartFspipeResponseObject, error) {
	log := logger.FromContext(ctx)

	fspipe.mu.Lock()
	defer fspipe.mu.Unlock()

	// Check if already running
	if fspipe.running {
		return oapi.StartFspipe409JSONResponse{
			ConflictErrorJSONResponse: oapi.ConflictErrorJSONResponse{
				Message: "fspipe daemon is already running",
			},
		}, nil
	}

	// Determine mount path (Chrome's download directory)
	mountPath := defaultFspipeMountPath
	if req.Body != nil && req.Body.MountPath != nil && *req.Body.MountPath != "" {
		mountPath = *req.Body.MountPath
	}

	// Determine health port
	healthPort := defaultFspipeHealthPort
	if req.Body != nil && req.Body.HealthPort != nil {
		healthPort = *req.Body.HealthPort
	}

	// Determine if S3 mode
	hasS3 := req.Body != nil && req.Body.S3Config != nil

	// Create mountpoint if it doesn't exist (with permissions accessible to all users)
	if err := os.MkdirAll(mountPath, 0777); err != nil {
		log.Error("failed to create fspipe mountpoint", "path", mountPath, "error", err)
		return oapi.StartFspipe500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to create mountpoint: %v", err),
			},
		}, nil
	}
	// Ensure the directory has proper permissions for Chrome to access
	os.Chmod(mountPath, 0777)

	var client transport.Transport
	var transportMode string
	var wsEndpoint string
	var s3Bucket string
	var listenerServer *listener.Server
	var listenerPort int
	var outputDir string

	if hasS3 {
		// S3/R2 mode - upload directly to cloud storage
		transportMode = "s3"
		s3Cfg := req.Body.S3Config

		region := "auto"
		if s3Cfg.Region != nil {
			region = *s3Cfg.Region
		}
		prefix := ""
		if s3Cfg.Prefix != nil {
			prefix = *s3Cfg.Prefix
		}

		s3Config := transport.S3Config{
			Endpoint:        s3Cfg.Endpoint,
			Bucket:          s3Cfg.Bucket,
			AccessKeyID:     s3Cfg.AccessKeyId,
			SecretAccessKey: s3Cfg.SecretAccessKey,
			Region:          region,
			Prefix:          prefix,
		}
		s3Bucket = s3Cfg.Bucket

		var err error
		client, err = transport.NewS3Client(s3Config)
		if err != nil {
			log.Error("failed to create S3 transport", "bucket", s3Cfg.Bucket, "error", err)
			return oapi.StartFspipe500JSONResponse{
				InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
					Message: fmt.Sprintf("failed to create S3 transport: %v", err),
				},
			}, nil
		}
	} else {
		// Default WebSocket mode - start broadcaster that external clients connect to
		transportMode = "websocket"
		listenerPort = defaultFspipeListenerPort

		// External endpoint URL for clients outside the container
		wsEndpoint = fmt.Sprintf("ws://0.0.0.0:%d/fspipe", listenerPort)

		// Create broadcaster - a WebSocket server that broadcasts to connected clients
		broadcasterAddr := fmt.Sprintf(":%d", listenerPort)
		broadcaster := transport.NewBroadcaster(broadcasterAddr, "/fspipe")

		// For now, allow operation without clients (fake ACKs) for backward compatibility
		// Set to true for strict mode where downloads fail if no client is connected
		broadcaster.SetRequireClient(false)

		// Enable fast mode: writes are fire-and-forget, only FileCreate waits for ACK
		// This gives much better throughput for large file downloads
		broadcaster.SetFastMode(true)

		client = broadcaster
	}

	// Connect transport
	if err := client.Connect(); err != nil {
		if listenerServer != nil {
			listenerServer.Stop()
		}
		client.Close()
		log.Error("failed to connect fspipe transport", "error", err)
		return oapi.StartFspipe500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to connect transport: %v", err),
			},
		}, nil
	}

	// Mount the FUSE filesystem
	fuseServer, err := daemon.Mount(mountPath, client)
	if err != nil {
		if listenerServer != nil {
			listenerServer.Stop()
		}
		client.Close()
		log.Error("failed to mount fspipe filesystem", "path", mountPath, "error", err)
		return oapi.StartFspipe500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to mount filesystem: %v", err),
			},
		}, nil
	}

	// Start health server
	healthAddr := fmt.Sprintf(":%d", healthPort)
	healthServer := health.NewServer(healthAddr)

	healthServer.RegisterCheck("transport", func() (health.Status, string) {
		state := client.State()
		switch state {
		case transport.StateConnected:
			return health.StatusHealthy, "connected"
		case transport.StateReconnecting:
			return health.StatusDegraded, "reconnecting"
		default:
			return health.StatusUnhealthy, state.String()
		}
	})

	healthServer.RegisterStats("transport", func() map[string]interface{} {
		stats := client.Stats()
		result := make(map[string]interface{})
		for k, v := range stats {
			result[k] = v
		}
		result["state"] = client.State().String()
		return result
	})

	if err := healthServer.Start(); err != nil {
		log.Warn("failed to start fspipe health server", "error", err)
	}

	// Store state
	fspipe.running = true
	fspipe.transportMode = transportMode
	fspipe.mountPath = mountPath
	fspipe.wsEndpoint = wsEndpoint
	fspipe.s3Bucket = s3Bucket
	fspipe.healthPort = healthPort
	fspipe.listenerPort = listenerPort
	fspipe.outputDir = outputDir
	fspipe.transport = client
	fspipe.fuseServer = fuseServer
	fspipe.healthServer = healthServer
	fspipe.listenerServer = listenerServer

	log.Info("fspipe daemon started", "mode", transportMode, "mount", mountPath)

	// Set Chrome download directory via enterprise policy (more reliable than command-line flag)
	policyManager := &policy.Policy{}
	if err := policyManager.SetDownloadDirectory(mountPath, true); err != nil {
		log.Warn("failed to set Chrome download directory policy", "error", err)
	} else {
		log.Info("set Chrome download directory policy", "path", mountPath)
	}

	// Restart Chrome to apply policy changes
	if err := s.restartChromiumAndWait(ctx, "fspipe setup"); err != nil {
		log.Warn("failed to restart Chrome for fspipe setup", "error", err)
	}

	// Build response
	response := oapi.FspipeStartResult{
		Running:       true,
		TransportMode: oapi.FspipeStartResultTransportMode(transportMode),
		MountPath:     mountPath,
	}

	if transportMode == "websocket" {
		response.WsEndpoint = &wsEndpoint
	} else {
		response.S3Bucket = &s3Bucket
	}

	healthEndpoint := fmt.Sprintf("http://localhost:%d", healthPort)
	response.HealthEndpoint = &healthEndpoint

	return oapi.StartFspipe200JSONResponse(response), nil
}

// StopFspipe stops the running fspipe daemon
func (s *ApiService) StopFspipe(ctx context.Context, req oapi.StopFspipeRequestObject) (oapi.StopFspipeResponseObject, error) {
	log := logger.FromContext(ctx)

	fspipe.mu.Lock()
	defer fspipe.mu.Unlock()

	if !fspipe.running {
		return oapi.StopFspipe400JSONResponse{
			BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "fspipe daemon is not running",
			},
		}, nil
	}

	// Stop health server
	if fspipe.healthServer != nil {
		fspipe.healthServer.Stop(ctx)
	}

	// Unmount filesystem
	if fspipe.fuseServer != nil {
		if err := fspipe.fuseServer.Unmount(); err != nil {
			log.Warn("failed to unmount fspipe filesystem", "error", err)
		}
	}

	// Close transport
	if fspipe.transport != nil {
		if err := fspipe.transport.Close(); err != nil {
			log.Warn("failed to close fspipe transport", "error", err)
		}
	}

	// Stop listener server (if running)
	if fspipe.listenerServer != nil {
		if err := fspipe.listenerServer.Stop(); err != nil {
			log.Warn("failed to stop fspipe listener", "error", err)
		}
	}

	// Clear download directory policy
	policyManager := &policy.Policy{}
	if err := policyManager.ClearDownloadDirectory(); err != nil {
		log.Warn("failed to clear Chrome download directory policy", "error", err)
	}

	// Reset state
	fspipe.running = false
	fspipe.transportMode = ""
	fspipe.mountPath = ""
	fspipe.wsEndpoint = ""
	fspipe.s3Bucket = ""
	fspipe.healthPort = 0
	fspipe.listenerPort = 0
	fspipe.outputDir = ""
	fspipe.transport = nil
	fspipe.fuseServer = nil
	fspipe.healthServer = nil
	fspipe.listenerServer = nil

	log.Info("fspipe daemon stopped")
	return oapi.StopFspipe200Response{}, nil
}

// GetFspipeStatus returns the current status of the fspipe daemon
func (s *ApiService) GetFspipeStatus(ctx context.Context, req oapi.GetFspipeStatusRequestObject) (oapi.GetFspipeStatusResponseObject, error) {
	fspipe.mu.RLock()
	defer fspipe.mu.RUnlock()

	if !fspipe.running {
		return oapi.GetFspipeStatus200JSONResponse(oapi.FspipeStatus{
			Running: false,
		}), nil
	}

	status := oapi.FspipeStatus{
		Running:   true,
		MountPath: &fspipe.mountPath,
	}

	// Set transport mode
	mode := oapi.FspipeStatusTransportMode(fspipe.transportMode)
	status.TransportMode = &mode

	// Set transport state
	if fspipe.transport != nil {
		stateStr := fspipe.transport.State().String()
		var state oapi.FspipeStatusTransportState
		switch stateStr {
		case "connected":
			state = oapi.Connected
		case "reconnecting":
			state = oapi.Reconnecting
		default:
			state = oapi.Disconnected
		}
		status.TransportState = &state

		// Get stats
		rawStats := fspipe.transport.Stats()
		stats := make(map[string]interface{})
		for k, v := range rawStats {
			stats[k] = v
		}
		status.Stats = &stats
	}

	// Set endpoint info
	if fspipe.transportMode == "websocket" && fspipe.wsEndpoint != "" {
		status.WsEndpoint = &fspipe.wsEndpoint
	}
	if fspipe.transportMode == "s3" && fspipe.s3Bucket != "" {
		status.S3Bucket = &fspipe.s3Bucket
	}

	return oapi.GetFspipeStatus200JSONResponse(status), nil
}
