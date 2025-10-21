package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

func TestExecutePlaywrightRequest_Validation(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		checkError     bool
	}{
		{
			name:           "empty code",
			requestBody:    `{"code": ""}`,
			expectedStatus: http.StatusBadRequest,
			checkError:     true,
		},
		{
			name:           "missing code field",
			requestBody:    `{}`,
			expectedStatus: http.StatusBadRequest,
			checkError:     true,
		},
		{
			name:           "invalid json",
			requestBody:    `{invalid}`,
			expectedStatus: http.StatusBadRequest,
			checkError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/playwright/execute", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")

			testLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			ctx := logger.AddToContext(context.Background(), testLogger)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			s.ExecutePlaywrightCode(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestExecutePlaywrightRequest_ValidCode(t *testing.T) {
	t.Skip("Skipping integration test that requires Playwright to be installed")

	s := &Service{}

	reqBody := ExecutePlaywrightRequest{
		Code: "return 'hello world';",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/playwright/execute", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	testLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx := logger.AddToContext(context.Background(), testLogger)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	s.ExecutePlaywrightCode(w, req)

	if w.Code != http.StatusOK {
		t.Logf("Response body: %s", w.Body.String())
	}

	var result ExecutePlaywrightResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Logf("Could not parse response as ExecutePlaywrightResult, this is expected if Playwright is not available")
	}
}
