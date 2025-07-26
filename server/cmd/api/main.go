package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/exec"
    "os/signal"
    "syscall"
    "time"

    "github.com/ghodss/yaml"
    "github.com/go-chi/chi/v5"
    chiMiddleware "github.com/go-chi/chi/v5/middleware"
    "github.com/go-chi/cors"
    "golang.org/x/sync/errgroup"

    serverpkg "github.com/onkernel/kernel-images/server"
    "github.com/onkernel/kernel-images/server/cmd/api/api"
    "github.com/onkernel/kernel-images/server/cmd/config"
    "github.com/onkernel/kernel-images/server/lib/logger"
    oapi "github.com/onkernel/kernel-images/server/lib/oapi"
    "github.com/onkernel/kernel-images/server/lib/recorder"
    "github.com/onkernel/kernel-images/server/lib/scaletozero"
)

func main() {
    slogger := slog.New(slog.NewTextHandler(os.Stdout, nil))

    cfg, err := config.Load()
    if err != nil {
        slogger.Error("failed to load configuration", "err", err)
        os.Exit(1)
    }
    slogger.Info("server configuration", "config", cfg)

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    mustFFmpeg()

    r := chi.NewRouter()

    r.Use(cors.Handler(cors.Options{
        AllowedOrigins:   []string{"*"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
        ExposedHeaders:   []string{"X-Recording-Started-At", "X-Recording-Finished-At"},
        AllowCredentials: true,
        MaxAge:           300,
    }))

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
        DisplayNum:  &cfg.DisplayNum,
        FrameRate:   &cfg.FrameRate,
        MaxSizeInMB: &cfg.MaxSizeInMB,
        OutputDir:   &cfg.OutputDir,
    }
    if err := defaultParams.Validate(); err != nil {
        slogger.Error("invalid default recording parameters", "err", err)
        os.Exit(1)
    }
    stz := scaletozero.NewUnikraftCloudController()

    apiService, err := api.New(
        recorder.NewFFmpegManager(),
        recorder.NewFFmpegRecorderFactory(cfg.PathToFFmpeg, defaultParams, stz),
    )
    if err != nil {
        slogger.Error("failed to create api service", "err", err)
        os.Exit(1)
    }

    strictHandler := oapi.NewStrictHandler(apiService, nil)
    oapi.HandlerFromMux(strictHandler, r)

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
        Addr:    fmt.Sprintf(":%d", cfg.Port),
        Handler: r,
    }

    go func() {
        slogger.Info("http server starting", "addr", srv.Addr)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            slogger.Error("http server failed", "err", err)
            stop()
        }
    }()

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
