package api

import (
	"context"
	"testing"

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
	"github.com/stretchr/testify/require"
)

func TestExecutePlaywrightRequest_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &ApiService{}

	tests := []struct {
		name        string
		code        string
		expectError bool
	}{
		{
			name:        "empty code",
			code:        "",
			expectError: true,
		},
		{
			name:        "valid code",
			code:        "return 'hello world';",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &oapi.ExecutePlaywrightCodeRequest{
				Code: tt.code,
			}
			resp, err := svc.ExecutePlaywrightCode(ctx, oapi.ExecutePlaywrightCodeRequestObject{Body: body})
			require.NoError(t, err, "ExecutePlaywrightCode returned error")

			if tt.expectError {
				_, ok := resp.(oapi.ExecutePlaywrightCode400JSONResponse)
				require.True(t, ok, "expected 400 response for empty code, got %T", resp)
			} else {
				// For valid code, we expect either 200 or 500 (if playwright is not available)
				// The actual execution is tested in e2e tests
				switch resp.(type) {
				case oapi.ExecutePlaywrightCode200JSONResponse:
					// Success case (if playwright is available)
				case oapi.ExecutePlaywrightCode500JSONResponse:
					// Expected if playwright is not available in test environment
				default:
					t.Errorf("unexpected response type: %T", resp)
				}
			}
		})
	}
}
