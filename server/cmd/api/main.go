package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/sync/errgroup"

	serverpkg "github.com/onkernel/kernel-images/server"
	"github.com/onkernel/kernel-images/server/cmd/api/api"
	"github.com/onkernel/kernel-images/server/cmd/config"
	"github.com/onkernel/kernel-images/server/lib/devtoolsproxy"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/onkernel/kernel-images/server/lib/recorder"
	"github.com/onkernel/kernel-images/server/lib/scaletozero"
)

func main() {
	slogger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Load configuration from environment variables
	config, err := config.Load()
	if err != nil {
		slogger.Error("failed to load configuration", "err", err)
		os.Exit(1)
	}
	slogger.Info("server configuration", "config", config)

	// context cancellation on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ensure ffmpeg is available
	mustFFmpeg()

	r := chi.NewRouter()
	r.Use(
		chiMiddleware.Logger,
		chiMiddleware.Recoverer,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctxWithLogger := logger.AddToContext(r.Context(), slogger)
				next.ServeHTTP(w, r.WithContext(ctxWithLogger))
			})
		},
	)

	defaultParams := recorder.FFmpegRecordingParams{
		DisplayNum:  &config.DisplayNum,
		FrameRate:   &config.FrameRate,
		MaxSizeInMB: &config.MaxSizeInMB,
		OutputDir:   &config.OutputDir,
	}
	if err := defaultParams.Validate(); err != nil {
		slogger.Error("invalid default recording parameters", "err", err)
		os.Exit(1)
	}
	stz := scaletozero.NewUnikraftCloudController()

	apiService, err := api.New(
		recorder.NewFFmpegManager(),
		recorder.NewFFmpegRecorderFactory(config.PathToFFmpeg, defaultParams, stz),
	)
	if err != nil {
		slogger.Error("failed to create api service", "err", err)
		os.Exit(1)
	}

	strictHandler := oapi.NewStrictHandler(apiService, nil)
	oapi.HandlerFromMux(strictHandler, r)

	// Register our custom endpoint handler
	r.Get("/screen/resolution", apiService.SetScreenResolutionHandler)

	// endpoints to expose the spec
	r.Get("/spec.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oai.openapi")
		w.Write(serverpkg.OpenAPIYAML)
	})
	r.Get("/spec.json", func(w http.ResponseWriter, r *http.Request) {
		jsonData, err := yaml.YAMLToJSON(serverpkg.OpenAPIYAML)
		if err != nil {
			http.Error(w, "failed to convert YAML to JSON", http.StatusInternalServerError)
			logger.FromContext(r.Context()).Error("failed to convert YAML to JSON", "err", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: r,
	}

	// DevTools WebSocket proxy setup: tail Chromium supervisord log and start WS server on :9222 only after upstream is found
	const chromiumLogPath = "/var/log/supervisord/chromium"
	upstreamMgr := devtoolsproxy.NewUpstreamManager(chromiumLogPath, slogger)
	upstreamMgr.Start(ctx)

	// wait up to 10 seconds for initial upstream; exit nonzero if not found
	if _, err := upstreamMgr.WaitForInitial(10 * time.Second); err != nil {
		slogger.Error("devtools upstream not available", "err", err)
		os.Exit(1)
	}

	rDevtools := chi.NewRouter()
	rDevtools.Use(
		chiMiddleware.Logger,
		chiMiddleware.Recoverer,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctxWithLogger := logger.AddToContext(r.Context(), slogger)
				next.ServeHTTP(w, r.WithContext(ctxWithLogger))
			})
		},
	)
	// Expose a minimal /json/version endpoint so clients that attempt to
	// resolve a browser websocket URL via HTTP can succeed. We map the
	// upstream path onto this proxy's host:port so clients connect back to us.
	rDevtools.Get("/json/version", func(w http.ResponseWriter, r *http.Request) {
		current := upstreamMgr.Current()
		if current == "" {
			http.Error(w, "upstream not ready", http.StatusServiceUnavailable)
			return
		}
		proxyWSURL := (&url.URL{Scheme: "ws", Host: r.Host}).String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": proxyWSURL,
		})
	})
	rDevtools.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		devtoolsproxy.WebSocketProxyHandler(upstreamMgr, slogger, config.LogCDPMessages, stz).ServeHTTP(w, r)
	})

	srvDevtools := &http.Server{
		Addr:    "0.0.0.0:9222",
		Handler: rDevtools,
	}

	go func() {
		slogger.Info("http server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slogger.Error("http server failed", "err", err)
			stop()
		}
	}()

	go func() {
		slogger.Info("devtools websocket proxy starting", "addr", srvDevtools.Addr)
		if err := srvDevtools.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slogger.Error("devtools websocket proxy failed", "err", err)
			stop()
		}
	}()

	// graceful shutdown
	<-ctx.Done()
	slogger.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	g, _ := errgroup.WithContext(shutdownCtx)

	g.Go(func() error {
		return srv.Shutdown(shutdownCtx)
	})
	g.Go(func() error {
		return apiService.Shutdown(shutdownCtx)
	})
	g.Go(func() error {
		upstreamMgr.Stop()
		return srvDevtools.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil {
		slogger.Error("server failed to shutdown", "err", err)
	}
}

func mustFFmpeg() {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		panic(fmt.Errorf("ffmpeg not found or not executable: %w", err))
	}
}
