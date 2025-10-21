package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

type ExecutePlaywrightRequest struct {
	Code       string `json:"code"`
	TimeoutSec *int   `json:"timeout_sec,omitempty"`
}

type ExecutePlaywrightResult struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
	Stdout  string      `json:"stdout,omitempty"`
	Stderr  string      `json:"stderr,omitempty"`
}

func (s *Service) ExecutePlaywrightCode(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	var req ExecutePlaywrightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	timeout := 30 * time.Second
	if req.TimeoutSec != nil && *req.TimeoutSec > 0 {
		timeout = time.Duration(*req.TimeoutSec) * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tsx", "/usr/local/lib/playwright-executor.ts", req.Code)

	output, err := cmd.CombinedOutput()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Error("playwright execution timed out", "timeout", timeout)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ExecutePlaywrightResult{
				Success: false,
				Error:   fmt.Sprintf("execution timed out after %v", timeout),
			})
			return
		}

		log.Error("playwright execution failed", "error", err, "output", string(output))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		var result ExecutePlaywrightResult
		if jsonErr := json.Unmarshal(output, &result); jsonErr == nil {
			json.NewEncoder(w).Encode(result)
		} else {
			json.NewEncoder(w).Encode(ExecutePlaywrightResult{
				Success: false,
				Error:   fmt.Sprintf("execution failed: %v", err),
				Stderr:  string(output),
			})
		}
		return
	}

	var result ExecutePlaywrightResult
	if err := json.Unmarshal(output, &result); err != nil {
		log.Error("failed to parse playwright output", "error", err, "output", string(output))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ExecutePlaywrightResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse output: %v", err),
			Stdout:  string(output),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
