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
	"github.com/onkernel/kernel-images/server/lib/nekoclient"
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

	stz := scaletozero.NewDebouncedController(scaletozero.NewUnikraftCloudController())
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
		scaletozero.Middleware(stz),
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

	// DevTools WebSocket upstream manager: tail Chromium supervisord log
	const chromiumLogPath = "/var/log/supervisord/chromium"
	upstreamMgr := devtoolsproxy.NewUpstreamManager(chromiumLogPath, slogger)
	upstreamMgr.Start(ctx)

	// Initialize Neko authenticated client
	adminPassword := os.Getenv("NEKO_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin" // Default from neko.yaml
	}
	nekoAuthClient, err := nekoclient.NewAuthClient("http://127.0.0.1:8080", "admin", adminPassword)
	if err != nil {
		slogger.Error("failed to create neko auth client", "err", err)
		os.Exit(1)
	}

	apiService, err := api.New(
		recorder.NewFFmpegManager(),
		recorder.NewFFmpegRecorderFactory(config.PathToFFmpeg, defaultParams, stz),
		upstreamMgr,
		stz,
		nekoAuthClient,
	)
	if err != nil {
		slogger.Error("failed to create api service", "err", err)
		os.Exit(1)
	}

	strictHandler := oapi.NewStrictHandler(apiService, nil)
	oapi.HandlerFromMux(strictHandler, r)

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
	// Raw attach endpoint (HTTP hijack) - not part of OpenAPI spec
	r.Get("/process/{process_id}/attach", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "process_id")
		apiService.HandleProcessAttach(w, r, id)
	})

	// Serve extension files for Chrome policy-installed extensions
	// This allows Chrome to download .crx and update.xml files via HTTP
	extensionsDir := "/home/kernel/extensions"
	r.Get("/extensions/*", func(w http.ResponseWriter, r *http.Request) {
		// Serve files from /home/kernel/extensions/
		fs := http.StripPrefix("/extensions/", http.FileServer(http.Dir(extensionsDir)))
		fs.ServeHTTP(w, r)
	})

	// Serve update.xml at root for Chrome enterprise policy
	// This serves the first update.xml found in any extension directory
	r.Get("/update.xml", func(w http.ResponseWriter, r *http.Request) {
		// Try to find update.xml in the first extension directory
		entries, err := os.ReadDir(extensionsDir)
		if err != nil {
			http.Error(w, "extensions directory not found", http.StatusNotFound)
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				updateXMLPath := fmt.Sprintf("%s/%s/update.xml", extensionsDir, entry.Name())
				if _, err := os.Stat(updateXMLPath); err == nil {
					http.ServeFile(w, r, updateXMLPath)
					return
				}
			}
		}

		http.Error(w, "update.xml not found", http.StatusNotFound)
	})

	// Serve CRX files at root for Chrome enterprise policy
	// This allows simple codebase URLs like http://host:port/extension-name.crx
	r.Get("/{filename}.crx", func(w http.ResponseWriter, r *http.Request) {
		// Extract the filename from the URL path
		filename := chi.URLParam(r, "filename") + ".crx"

		// Search for the CRX file in all extension directories
		entries, err := os.ReadDir(extensionsDir)
		if err != nil {
			http.Error(w, "extensions directory not found", http.StatusNotFound)
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				crxPath := fmt.Sprintf("%s/%s/%s", extensionsDir, entry.Name(), filename)
				if _, err := os.Stat(crxPath); err == nil {
					http.ServeFile(w, r, crxPath)
					return
				}
			}
		}

		http.Error(w, "crx file not found", http.StatusNotFound)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: r,
	}

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
		scaletozero.Middleware(stz),
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
