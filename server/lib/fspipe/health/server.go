package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
)

// Status represents the health status
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Check is a health check function
type Check func() (Status, string)

// StatsProvider provides statistics
type StatsProvider func() map[string]interface{}

// Server provides health and metrics endpoints
type Server struct {
	addr   string
	server *http.Server

	mu     sync.RWMutex
	checks map[string]Check
	stats  map[string]StatsProvider

	startTime time.Time
}

// NewServer creates a new health server
func NewServer(addr string) *Server {
	s := &Server{
		addr:      addr,
		checks:    make(map[string]Check),
		stats:     make(map[string]StatsProvider),
		startTime: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/health/live", s.handleLiveness)
	mux.HandleFunc("/health/ready", s.handleReadiness)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/stats", s.handleStats)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// RegisterCheck adds a health check
func (s *Server) RegisterCheck(name string, check Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[name] = check
}

// RegisterStats adds a stats provider
func (s *Server) RegisterStats(name string, provider StatsProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats[name] = provider
}

// Start begins serving
func (s *Server) Start() error {
	go func() {
		logging.Info("Health server listening on %s", s.addr)
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			logging.Error("Health server error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	overall := StatusHealthy
	results := make(map[string]interface{})

	for name, check := range s.checks {
		status, msg := check()
		results[name] = map[string]interface{}{
			"status":  status,
			"message": msg,
		}

		if status == StatusUnhealthy {
			overall = StatusUnhealthy
		} else if status == StatusDegraded && overall == StatusHealthy {
			overall = StatusDegraded
		}
	}

	response := map[string]interface{}{
		"status":    overall,
		"checks":    results,
		"uptime":    time.Since(s.startTime).String(),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if overall != StatusHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "alive",
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, check := range s.checks {
		status, msg := check()
		if status == StatusUnhealthy {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "not_ready",
				"reason":  name,
				"message": msg,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Prometheus-style metrics
	w.Header().Set("Content-Type", "text/plain")

	fmt.Fprintf(w, "# HELP fspipe_uptime_seconds Uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE fspipe_uptime_seconds gauge\n")
	fmt.Fprintf(w, "fspipe_uptime_seconds %f\n", time.Since(s.startTime).Seconds())

	for name, provider := range s.stats {
		stats := provider()
		for key, value := range stats {
			metricName := fmt.Sprintf("fspipe_%s_%s", name, key)

			switch v := value.(type) {
			case uint64:
				fmt.Fprintf(w, "%s %d\n", metricName, v)
			case int64:
				fmt.Fprintf(w, "%s %d\n", metricName, v)
			case int:
				fmt.Fprintf(w, "%s %d\n", metricName, v)
			case float64:
				fmt.Fprintf(w, "%s %f\n", metricName, v)
			case string:
				// Skip strings in prometheus format
			}
		}
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allStats := make(map[string]interface{})
	allStats["uptime"] = time.Since(s.startTime).String()
	allStats["timestamp"] = time.Now().Format(time.RFC3339)

	for name, provider := range s.stats {
		allStats[name] = provider()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allStats)
}
