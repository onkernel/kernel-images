package benchmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// CDPRuntimeBenchmark performs runtime benchmarks on the CDP proxy
type CDPRuntimeBenchmark struct {
	logger      *slog.Logger
	proxyURL    string
	concurrency int
}

// NewCDPRuntimeBenchmark creates a new CDP runtime benchmark
func NewCDPRuntimeBenchmark(logger *slog.Logger, proxyURL string, concurrency int) *CDPRuntimeBenchmark {
	return &CDPRuntimeBenchmark{
		logger:      logger,
		proxyURL:    proxyURL,
		concurrency: concurrency,
	}
}

// Run executes the CDP benchmark for a fixed 5-second duration
// Tests both proxied (9222) and direct (9223) CDP endpoints
func (b *CDPRuntimeBenchmark) Run(ctx context.Context, duration time.Duration) (*CDPProxyResults, error) {
	// Fixed 5-second duration for CDP benchmarks (measures req/sec throughput)
	const cdpDuration = 5 * time.Second
	b.logger.Info("starting CDP benchmark (proxied + direct)", "duration", cdpDuration, "concurrency", b.concurrency)

	// Get baseline memory
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	// Parse proxy URL and derive direct URL
	proxiedURL := b.proxyURL
	directURL := "http://localhost:9223" // Direct Chrome CDP endpoint

	// Fetch WebSocket URLs from /json/version endpoints
	proxiedWSURL, err := fetchCDPWebSocketURL(proxiedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxied CDP WebSocket URL: %w", err)
	}
	b.logger.Info("resolved proxied WebSocket URL", "url", proxiedWSURL)

	directWSURL, err := fetchCDPWebSocketURL(directURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get direct CDP WebSocket URL: %w", err)
	}
	b.logger.Info("resolved direct WebSocket URL", "url", directWSURL)

	// Benchmark proxied endpoint (kernel-images proxy on port 9222)
	b.logger.Info("benchmarking proxied CDP endpoint", "url", proxiedURL)
	proxiedResults := b.runWorkers(ctx, proxiedWSURL, cdpDuration)

	// Benchmark direct endpoint (Chrome CDP on port 9223)
	b.logger.Info("benchmarking direct CDP endpoint", "url", directURL)
	directResults := b.runWorkers(ctx, directWSURL, cdpDuration)

	// Get final memory
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	// Calculate memory metrics
	baselineMemMB := float64(memStatsBefore.Alloc) / 1024 / 1024
	finalMemMB := float64(memStatsAfter.Alloc) / 1024 / 1024
	perConnectionMemMB := (finalMemMB - baselineMemMB) / float64(b.concurrency)

	// Calculate proxy overhead
	proxyOverhead := 0.0
	if directResults.ThroughputMsgsPerSec > 0 {
		proxyOverhead = ((directResults.ThroughputMsgsPerSec - proxiedResults.ThroughputMsgsPerSec) / directResults.ThroughputMsgsPerSec) * 100.0
	}

	// Return results with both direct and proxied endpoint metrics
	// Overall metrics use proxied results for backward compatibility
	return &CDPProxyResults{
		ThroughputMsgsPerSec:  proxiedResults.ThroughputMsgsPerSec,
		LatencyMS:             proxiedResults.LatencyMS,
		ConcurrentConnections: b.concurrency,
		MemoryMB: MemoryMetrics{
			Baseline:      baselineMemMB,
			PerConnection: perConnectionMemMB,
		},
		MessageSizeBytes: proxiedResults.MessageSizeBytes,
		// No root-level Scenarios - they're in ProxiedEndpoint and DirectEndpoint
		ProxiedEndpoint: &CDPEndpointResults{
			EndpointURL:          proxiedURL,
			ThroughputMsgsPerSec: proxiedResults.ThroughputMsgsPerSec,
			LatencyMS:            proxiedResults.LatencyMS,
			Scenarios:            proxiedResults.Scenarios,
		},
		DirectEndpoint: &CDPEndpointResults{
			EndpointURL:          directURL,
			ThroughputMsgsPerSec: directResults.ThroughputMsgsPerSec,
			LatencyMS:            directResults.LatencyMS,
			Scenarios:            directResults.Scenarios,
		},
		ProxyOverheadPercent: proxyOverhead,
	}, nil
}

// fetchCDPWebSocketURL fetches the WebSocket debugger URL from a CDP endpoint
// by querying the /json/version endpoint, following the standard CDP protocol
func fetchCDPWebSocketURL(baseURL string) (string, error) {
	// Ensure baseURL has a scheme
	if u, err := url.Parse(baseURL); err == nil && u.Scheme == "" {
		baseURL = "http://" + baseURL
	}

	// Construct /json/version URL
	versionURL := baseURL + "/json/version"

	// Make HTTP request
	resp, err := http.Get(versionURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", versionURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, versionURL, string(body))
	}

	// Parse JSON response
	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return "", fmt.Errorf("failed to decode JSON from %s: %w", versionURL, err)
	}

	if versionInfo.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("webSocketDebuggerUrl not found in response from %s", versionURL)
	}

	return versionInfo.WebSocketDebuggerURL, nil
}

type workerResults struct {
	ThroughputMsgsPerSec float64
	LatencyMS            LatencyMetrics
	MessageSizeBytes     MessageSizeMetrics
	Scenarios            []CDPScenarioResult
}

type scenarioStats struct {
	Name        string
	Description string
	Category    string
	Operations  atomic.Int64
	Failures    atomic.Int64
	Latencies   []float64
	LatenciesMu sync.Mutex
}

func (b *CDPRuntimeBenchmark) runWorkers(ctx context.Context, wsURL string, duration time.Duration) workerResults {
	benchCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	// Define test scenarios with named operations
	type testScenario struct {
		Name        string
		Description string
		Category    string
		Message     []byte
	}

	scenarios := []testScenario{
		// Runtime operations - JavaScript evaluation and object inspection
		{
			Name:        "Runtime.evaluate",
			Description: "Evaluate simple JavaScript expression",
			Category:    "Runtime",
			Message:     []byte(`{"id":1,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`),
		},
		{
			Name:        "Runtime.evaluate-complex",
			Description: "Evaluate complex JavaScript with DOM access",
			Category:    "Runtime",
			Message:     []byte(`{"id":2,"method":"Runtime.evaluate","params":{"expression":"document.querySelector('body').children.length"}}`),
		},
		{
			Name:        "Runtime.getProperties",
			Description: "Get runtime object properties",
			Category:    "Runtime",
			Message:     []byte(`{"id":3,"method":"Runtime.getProperties","params":{"objectId":"1"}}`),
		},
		{
			Name:        "Runtime.callFunctionOn",
			Description: "Call function on remote object",
			Category:    "Runtime",
			Message:     []byte(`{"id":4,"method":"Runtime.callFunctionOn","params":{"objectId":"1","functionDeclaration":"function(){return this;}"}}`),
		},

		// DOM operations - Document structure and element queries
		{
			Name:        "DOM.getDocument",
			Description: "Retrieve the root DOM document structure",
			Category:    "DOM",
			Message:     []byte(`{"id":5,"method":"DOM.getDocument","params":{}}`),
		},
		{
			Name:        "DOM.querySelector",
			Description: "Query DOM elements by CSS selector",
			Category:    "DOM",
			Message:     []byte(`{"id":6,"method":"DOM.querySelector","params":{"nodeId":1,"selector":"body"}}`),
		},
		{
			Name:        "DOM.getAttributes",
			Description: "Get attributes of a DOM node",
			Category:    "DOM",
			Message:     []byte(`{"id":7,"method":"DOM.getAttributes","params":{"nodeId":1}}`),
		},
		{
			Name:        "DOM.getOuterHTML",
			Description: "Get outer HTML of a node",
			Category:    "DOM",
			Message:     []byte(`{"id":8,"method":"DOM.getOuterHTML","params":{"nodeId":1}}`),
		},

		// Page operations - Navigation, resources, and page state
		{
			Name:        "Page.getNavigationHistory",
			Description: "Retrieve page navigation history",
			Category:    "Page",
			Message:     []byte(`{"id":9,"method":"Page.getNavigationHistory","params":{}}`),
		},
		{
			Name:        "Page.getResourceTree",
			Description: "Get page resource tree structure",
			Category:    "Page",
			Message:     []byte(`{"id":10,"method":"Page.getResourceTree","params":{}}`),
		},
		{
			Name:        "Page.getFrameTree",
			Description: "Get frame tree structure",
			Category:    "Page",
			Message:     []byte(`{"id":11,"method":"Page.getFrameTree","params":{}}`),
		},
		{
			Name:        "Page.captureScreenshot",
			Description: "Capture page screenshot via CDP",
			Category:    "Page",
			Message:     []byte(`{"id":12,"method":"Page.captureScreenshot","params":{"format":"png"}}`),
		},

		// Network operations - Request/response inspection
		{
			Name:        "Network.getCookies",
			Description: "Retrieve browser cookies",
			Category:    "Network",
			Message:     []byte(`{"id":13,"method":"Network.getCookies","params":{}}`),
		},
		{
			Name:        "Network.getAllCookies",
			Description: "Get all cookies from all contexts",
			Category:    "Network",
			Message:     []byte(`{"id":14,"method":"Network.getAllCookies","params":{}}`),
		},

		// Performance operations - Metrics and profiling
		{
			Name:        "Performance.getMetrics",
			Description: "Get runtime performance metrics",
			Category:    "Performance",
			Message:     []byte(`{"id":15,"method":"Performance.getMetrics","params":{}}`),
		},

		// Target operations - Tab and context management
		{
			Name:        "Target.getTargets",
			Description: "List all available targets",
			Category:    "Target",
			Message:     []byte(`{"id":16,"method":"Target.getTargets","params":{}}`),
		},
		{
			Name:        "Target.getTargetInfo",
			Description: "Get information about specific target",
			Category:    "Target",
			Message:     []byte(`{"id":17,"method":"Target.getTargetInfo","params":{"targetId":"page"}}`),
		},

		// Browser operations - Browser-level information
		{
			Name:        "Browser.getVersion",
			Description: "Get browser version information",
			Category:    "Browser",
			Message:     []byte(`{"id":18,"method":"Browser.getVersion","params":{}}`),
		},
		// Note: Browser.getHistograms and SystemInfo methods removed - they return huge responses or are unsupported
	}

	// Initialize scenario tracking
	scenarioStatsMap := make([]*scenarioStats, len(scenarios))
	for i, scenario := range scenarios {
		scenarioStatsMap[i] = &scenarioStats{
			Name:        scenario.Name,
			Description: scenario.Description,
			Category:    scenario.Category,
			Latencies:   make([]float64, 0, 1000),
		}
	}

	var (
		totalOps     atomic.Int64
		latencies    []float64
		latenciesMu  sync.Mutex
		wg           sync.WaitGroup
	)

	startTime := time.Now()

	// Start workers
	for i := 0; i < b.concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			conn, _, err := websocket.Dial(benchCtx, wsURL, nil)
			if err != nil {
				b.logger.Error("failed to dial proxy", "worker", workerID, "err", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "")

			msgIdx := 0
			for {
				select {
				case <-benchCtx.Done():
					return
				default:
				}

				scenarioIdx := msgIdx % len(scenarios)
				msg := scenarios[scenarioIdx].Message
				stats := scenarioStatsMap[scenarioIdx]
				msgIdx++

				start := time.Now()
				if err := conn.Write(benchCtx, websocket.MessageText, msg); err != nil {
					if benchCtx.Err() != nil {
						return
					}
					stats.Failures.Add(1)
					b.logger.Error("write failed", "worker", workerID, "scenario", stats.Name, "err", err)
					return
				}

				if _, _, err := conn.Read(benchCtx); err != nil {
					if benchCtx.Err() != nil {
						return
					}
					stats.Failures.Add(1)
					b.logger.Error("read failed", "worker", workerID, "scenario", stats.Name, "err", err)
					return
				}

				latency := time.Since(start)
				latencyMs := float64(latency.Microseconds()) / 1000.0

				// Track overall stats
				totalOps.Add(1)
				latenciesMu.Lock()
				latencies = append(latencies, latencyMs)
				latenciesMu.Unlock()

				// Track scenario-specific stats
				stats.Operations.Add(1)
				stats.LatenciesMu.Lock()
				stats.Latencies = append(stats.Latencies, latencyMs)
				stats.LatenciesMu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	ops := totalOps.Load()

	// Calculate overall throughput
	throughput := float64(ops) / elapsed.Seconds()

	// Calculate overall latency percentiles
	latencyMetrics := calculatePercentiles(latencies)

	// Calculate per-scenario results
	scenarioResults := make([]CDPScenarioResult, len(scenarioStatsMap))
	for i, stats := range scenarioStatsMap {
		operations := stats.Operations.Load()
		failures := stats.Failures.Load()
		successRate := 100.0
		if operations > 0 {
			successRate = (float64(operations-failures) / float64(operations)) * 100.0
		}

		scenarioResults[i] = CDPScenarioResult{
			Name:                stats.Name,
			Description:         stats.Description,
			Category:            stats.Category,
			OperationCount:      operations,
			ThroughputOpsPerSec: float64(operations) / elapsed.Seconds(),
			LatencyMS:           calculatePercentiles(stats.Latencies),
			SuccessRate:         successRate,
		}
	}

	// Message size metrics (approximate based on test messages)
	messageSizes := MessageSizeMetrics{
		Min: 50,
		Max: 200,
		Avg: 100,
	}

	return workerResults{
		ThroughputMsgsPerSec: throughput,
		LatencyMS:            latencyMetrics,
		MessageSizeBytes:     messageSizes,
		Scenarios:            scenarioResults,
	}
}

func calculatePercentiles(values []float64) LatencyMetrics {
	if len(values) == 0 {
		return LatencyMetrics{}
	}

	sort.Float64s(values)

	p50Idx := int(math.Floor(float64(len(values)) * 0.50))
	p95Idx := int(math.Floor(float64(len(values)) * 0.95))
	p99Idx := int(math.Floor(float64(len(values)) * 0.99))

	if p50Idx >= len(values) {
		p50Idx = len(values) - 1
	}
	if p95Idx >= len(values) {
		p95Idx = len(values) - 1
	}
	if p99Idx >= len(values) {
		p99Idx = len(values) - 1
	}

	return LatencyMetrics{
		P50: values[p50Idx],
		P95: values[p95Idx],
		P99: values[p99Idx],
	}
}

// CDPMessage represents a generic CDP message
type CDPMessage struct {
	ID     int                    `json:"id"`
	Method string                 `json:"method,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  *CDPError              `json:"error,omitempty"`
}

// CDPError represents a CDP error response
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
