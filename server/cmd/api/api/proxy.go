package api

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

const proxyConfigPath = "/chromium/proxy-config.json"

var (
	proxyConfigMu sync.RWMutex
	proxyConfig   *oapi.ProxyConfig
)

// GetProxyConfig returns the current proxy configuration.
func (s *ApiService) GetProxyConfig(ctx context.Context, _ oapi.GetProxyConfigRequestObject) (oapi.GetProxyConfigResponseObject, error) {
	log := logger.FromContext(ctx)

	proxyConfigMu.RLock()
	defer proxyConfigMu.RUnlock()

	// If we have a cached config, return it
	if proxyConfig != nil {
		log.Info("returning cached proxy config", "host", stringVal(proxyConfig.Host))
		return oapi.GetProxyConfig200JSONResponse(*proxyConfig), nil
	}

	// Try to load from file
	data, err := os.ReadFile(proxyConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist
			log.Info("no proxy config found, returning empty config")
			return oapi.GetProxyConfig200JSONResponse(oapi.ProxyConfig{}), nil
		}
		log.Error("failed to read proxy config", "error", err)
		return oapi.GetProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to read proxy config"}}, nil
	}

	var cfg oapi.ProxyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Error("failed to parse proxy config", "error", err)
		return oapi.GetProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to parse proxy config"}}, nil
	}

	log.Info("returning proxy config from file", "host", stringVal(cfg.Host))
	return oapi.GetProxyConfig200JSONResponse(cfg), nil
}

// SetProxyConfig sets the proxy configuration.
func (s *ApiService) SetProxyConfig(ctx context.Context, request oapi.SetProxyConfigRequestObject) (oapi.SetProxyConfigResponseObject, error) {
	log := logger.FromContext(ctx)

	if request.Body == nil {
		return oapi.SetProxyConfig400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "request body required"}}, nil
	}

	cfg := request.Body

	// Validate required fields
	if cfg.Host == nil || *cfg.Host == "" {
		return oapi.SetProxyConfig400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "host is required"}}, nil
	}
	if cfg.Port == nil || *cfg.Port == 0 {
		return oapi.SetProxyConfig400JSONResponse{BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{Message: "port is required"}}, nil
	}

	// Set default scheme if not provided
	if cfg.Scheme == nil {
		defaultScheme := oapi.Http
		cfg.Scheme = &defaultScheme
	}

	// Set default bypass list if not provided
	if cfg.BypassList == nil {
		cfg.BypassList = &[]string{"localhost", "127.0.0.1"}
	}

	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()

	// Save to file
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Error("failed to marshal proxy config", "error", err)
		return oapi.SetProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to marshal proxy config"}}, nil
	}

	// Ensure the directory exists
	if err := os.MkdirAll("/chromium", 0o755); err != nil {
		log.Error("failed to create chromium dir", "error", err)
		return oapi.SetProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to create chromium dir"}}, nil
	}

	if err := os.WriteFile(proxyConfigPath, data, 0o644); err != nil {
		log.Error("failed to write proxy config", "error", err)
		return oapi.SetProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to write proxy config"}}, nil
	}

	// Update cache
	proxyConfig = cfg

	log.Info("proxy config saved", "host", *cfg.Host, "port", *cfg.Port)
	return oapi.SetProxyConfig200JSONResponse(*cfg), nil
}

// DeleteProxyConfig clears the proxy configuration.
func (s *ApiService) DeleteProxyConfig(ctx context.Context, _ oapi.DeleteProxyConfigRequestObject) (oapi.DeleteProxyConfigResponseObject, error) {
	log := logger.FromContext(ctx)

	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()

	// Clear cache
	proxyConfig = nil

	// Remove file
	if err := os.Remove(proxyConfigPath); err != nil && !os.IsNotExist(err) {
		log.Error("failed to remove proxy config file", "error", err)
		return oapi.DeleteProxyConfig500JSONResponse{InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{Message: "failed to remove proxy config file"}}, nil
	}

	log.Info("proxy config cleared")
	return oapi.DeleteProxyConfig204Response{}, nil
}

// stringVal returns the value of a string pointer or empty string if nil
func stringVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
