package benchmarks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestSendCDPCommand_Success(t *testing.T) {
	// Create a test WebSocket server that echoes back a success response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to accept websocket: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read the request
		ctx := context.Background()
		var msg CDPMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("Failed to read message: %v", err)
		}

		// Send back a success response
		response := CDPMessage{
			ID: msg.ID,
			Result: map[string]interface{}{
				"value": "test result",
			},
		}
		if err := wsjson.Write(ctx, conn, response); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Connect to the test server
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Test sendCDPCommand
	session := &cdpSession{conn: conn}
	response, err := session.send(ctx, "Test.method", map[string]interface{}{"key": "value"}, false)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
	}
	if response.ID != 1 {
		t.Errorf("Expected ID 1, got %d", response.ID)
	}
	if response.Result == nil {
		t.Error("Expected result, got nil")
	}
}

func TestSendCDPCommand_ErrorResponse(t *testing.T) {
	// Create a test WebSocket server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to accept websocket: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read the request
		ctx := context.Background()
		var msg CDPMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("Failed to read message: %v", err)
		}

		// Send back an error response
		response := CDPMessage{
			ID: msg.ID,
			Error: &CDPError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
		if err := wsjson.Write(ctx, conn, response); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Connect to the test server
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Test sendCDPCommand with error response
	session := &cdpSession{conn: conn}
	response, err := session.send(ctx, "Test.method", nil, false)
	if err == nil {
		t.Error("Expected error, got nil")
	}
	if response == nil {
		t.Fatal("Expected response even with error, got nil")
	}
	if response.Error == nil {
		t.Error("Expected error in response, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid params") {
		t.Errorf("Expected error message to contain 'Invalid params', got: %v", err)
	}
}

func TestCDPMessage_Marshal(t *testing.T) {
	msg := CDPMessage{
		ID:        123,
		Method:    "Runtime.evaluate",
		SessionID: "session-1",
		Params: map[string]interface{}{
			"expression": "1+1",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var unmarshaled CDPMessage
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.ID != msg.ID {
		t.Errorf("Expected ID %d, got %d", msg.ID, unmarshaled.ID)
	}
	if unmarshaled.Method != msg.Method {
		t.Errorf("Expected method %s, got %s", msg.Method, unmarshaled.Method)
	}
}

func TestCalculatePercentiles(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   LatencyMetrics
	}{
		{
			name:   "empty slice",
			values: []float64{},
			want:   LatencyMetrics{},
		},
		{
			name:   "single value",
			values: []float64{100},
			want: LatencyMetrics{
				P50: 100,
				P95: 100,
				P99: 100,
			},
		},
		{
			name:   "multiple values",
			values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			want: LatencyMetrics{
				P50: 6,
				P95: 10,
				P99: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePercentiles(tt.values)
			if got.P50 != tt.want.P50 {
				t.Errorf("P50: got %v, want %v", got.P50, tt.want.P50)
			}
			if got.P95 != tt.want.P95 {
				t.Errorf("P95: got %v, want %v", got.P95, tt.want.P95)
			}
			if got.P99 != tt.want.P99 {
				t.Errorf("P99: got %v, want %v", got.P99, tt.want.P99)
			}
		})
	}
}
