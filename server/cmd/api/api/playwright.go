package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/onkernel/kernel-images/server/lib/logger"
	"github.com/onkernel/kernel-images/server/lib/oapi"
)

// ExecutePlaywrightCode implements the Playwright code execution endpoint
func (s *ApiService) ExecutePlaywrightCode(ctx context.Context, request oapi.ExecutePlaywrightCodeRequestObject) (oapi.ExecutePlaywrightCodeResponseObject, error) {
	// Serialize Playwright execution - only one execution at a time
	s.playwrightMu.Lock()
	defer s.playwrightMu.Unlock()

	log := logger.FromContext(ctx)

	// Validate request
	if request.Body == nil || request.Body.Code == "" {
		return oapi.ExecutePlaywrightCode400JSONResponse{
			BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: "code is required",
			},
		}, nil
	}

	// Determine timeout (default to 60 seconds)
	timeout := 60 * time.Second
	if request.Body.TimeoutSec != nil && *request.Body.TimeoutSec > 0 {
		timeout = time.Duration(*request.Body.TimeoutSec) * time.Second
	}

	// Create a temporary file for the user code
	tmpFile, err := os.CreateTemp("", "playwright-code-*.ts")
	if err != nil {
		log.Error("failed to create temp file", "error", err)
		return oapi.ExecutePlaywrightCode500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to create temp file: %v", err),
			},
		}, nil
	}
	tmpFilePath := tmpFile.Name()
	defer os.Remove(tmpFilePath) // Clean up the temp file

	// Write the user code to the temp file
	if _, err := tmpFile.WriteString(request.Body.Code); err != nil {
		tmpFile.Close()
		log.Error("failed to write code to temp file", "error", err)
		return oapi.ExecutePlaywrightCode500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: fmt.Sprintf("failed to write code to temp file: %v", err),
			},
		}, nil
	}
	tmpFile.Close()

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the Playwright code via the executor script
	cmd := exec.CommandContext(execCtx, "tsx", "/usr/local/lib/playwright-executor.ts", tmpFilePath)

	output, err := cmd.CombinedOutput()

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			log.Error("playwright execution timed out", "timeout", timeout)
			success := false
			errorMsg := fmt.Sprintf("execution timed out after %v", timeout)
			return oapi.ExecutePlaywrightCode200JSONResponse{
				Success: success,
				Error:   &errorMsg,
			}, nil
		}

		log.Error("playwright execution failed", "error", err)

		// Try to parse the error output as JSON
		var result struct {
			Success bool        `json:"success"`
			Result  interface{} `json:"result,omitempty"`
			Error   string      `json:"error,omitempty"`
			Stack   string      `json:"stack,omitempty"`
		}
		if jsonErr := json.Unmarshal(output, &result); jsonErr == nil {
			success := result.Success
			errorMsg := result.Error
			stderr := string(output)
			return oapi.ExecutePlaywrightCode200JSONResponse{
				Success: success,
				Error:   &errorMsg,
				Stderr:  &stderr,
			}, nil
		}

		// If we can't parse the output, return a generic error
		success := false
		errorMsg := fmt.Sprintf("execution failed: %v", err)
		stderr := string(output)
		return oapi.ExecutePlaywrightCode200JSONResponse{
			Success: success,
			Error:   &errorMsg,
			Stderr:  &stderr,
		}, nil
	}

	// Parse successful output
	var result struct {
		Success bool        `json:"success"`
		Result  interface{} `json:"result,omitempty"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		log.Error("failed to parse playwright output", "error", err)
		success := false
		errorMsg := fmt.Sprintf("failed to parse output: %v", err)
		stdout := string(output)
		return oapi.ExecutePlaywrightCode200JSONResponse{
			Success: success,
			Error:   &errorMsg,
			Stdout:  &stdout,
		}, nil
	}

	return oapi.ExecutePlaywrightCode200JSONResponse{
		Success: result.Success,
		Result:  &result.Result,
	}, nil
}
