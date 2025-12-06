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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	benchmarkPageHTML = `<!doctype html><html><head><title>Onkernel Benchmark</title></head><body><div id="benchmark-root" data-value="42">benchmark</div><script>window.benchmarkCounter=0;window.bumpCounter=()=>++window.benchmarkCounter;</script></body></html>`
	maxErrorSamples   = 5
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

// Run executes the CDP benchmark
func (b *CDPRuntimeBenchmark) Run(ctx context.Context, duration time.Duration) (*CDPProxyResults, error) {
	benchmarkDuration := 40 * time.Second
	if duration > 0 {
		benchmarkDuration = duration
	}

	b.logger.Info("starting CDP benchmark", "duration", benchmarkDuration, "concurrency", b.concurrency)

	// Get baseline memory
	baselineMemMB, err := GetProcessRSSMemoryMB()
	if err != nil {
		b.logger.Warn("failed to read baseline RSS", "err", err)
		baselineMemMB = 0
	}

	// Benchmark proxied endpoint
	proxiedURL := b.proxyURL
	b.logger.Info("benchmarking proxied CDP endpoint", "url", proxiedURL)
	proxiedResults, err := b.benchmarkEndpoint(ctx, proxiedURL, benchmarkDuration)
	if err != nil {
		return nil, fmt.Errorf("proxied endpoint failed: %w", err)
	}

	// Benchmark direct endpoint
	directURL := "http://localhost:9223"
	b.logger.Info("benchmarking direct CDP endpoint", "url", directURL)
	directResults, err := b.benchmarkEndpoint(ctx, directURL, benchmarkDuration)
	if err != nil {
		return nil, fmt.Errorf("direct endpoint failed: %w", err)
	}

	// Get final memory
	finalMemMB, err := GetProcessRSSMemoryMB()
	if err != nil {
		b.logger.Warn("failed to read final RSS", "err", err)
		finalMemMB = baselineMemMB
	}

	// Calculate memory metrics
	memDeltaMB := finalMemMB - baselineMemMB
	if memDeltaMB < 0 {
		memDeltaMB = 0
	}
	perConnectionMemMB := 0.0
	if b.concurrency > 0 {
		perConnectionMemMB = memDeltaMB / float64(b.concurrency)
	}

	// Calculate proxy overhead
	proxyOverhead := 0.0
	if directResults.TotalThroughputOpsPerSec > 0 {
		proxyOverhead = ((directResults.TotalThroughputOpsPerSec - proxiedResults.TotalThroughputOpsPerSec) / directResults.TotalThroughputOpsPerSec) * 100.0
	}

	return &CDPProxyResults{
		ConcurrentConnections: b.concurrency,
		MemoryMB: MemoryMetrics{
			Baseline:      baselineMemMB,
			PerConnection: perConnectionMemMB,
		},
		ProxiedEndpoint:      proxiedResults,
		DirectEndpoint:       directResults,
		ProxyOverheadPercent: proxyOverhead,
	}, nil
}

// cdpScenario defines a CDP scenario to benchmark
type cdpScenario struct {
	Name        string
	Category    string
	Description string
	Run         func(context.Context, *cdpSession) error
	Duration    time.Duration // if >0, run as many iterations as possible within this duration
	Iterations  int           // if >0, run exactly this many iterations (used for heavy pages)
	Timeout     time.Duration // per-iteration timeout
	Type        string
}

// benchmarkEndpoint benchmarks a single CDP endpoint
func (b *CDPRuntimeBenchmark) benchmarkEndpoint(ctx context.Context, baseURL string, duration time.Duration) (*CDPEndpointResults, error) {
	wsURL, err := fetchBrowserWebSocketURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get WebSocket URL: %w", err)
	}
	b.logger.Info("resolved WebSocket URL", "url", wsURL)

	scenarios := benchmarkScenarios()
	results := b.runWorkload(ctx, wsURL, scenarios, duration)
	results.EndpointURL = baseURL

	return results, nil
}

// scenarioStats tracks per-scenario statistics
type scenarioStats struct {
	Name         string
	Description  string
	Category     string
	Type         string
	Attempts     atomic.Int64
	Successes    atomic.Int64
	Failures     atomic.Int64
	Latencies    []float64
	ErrorSamples []string
	DurationNS   atomic.Int64
	EventCount   atomic.Int64
	mu           sync.Mutex
}

// runWorkload runs deterministic CDP scenarios with separate connections per worker
func (b *CDPRuntimeBenchmark) runWorkload(ctx context.Context, wsURL string, scenarios []cdpScenario, duration time.Duration) *CDPEndpointResults {
	benchCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	// Initialize scenario tracking
	scenarioStatsMap := make(map[string]*scenarioStats)
	for _, scenario := range scenarios {
		scenarioStatsMap[scenario.Name] = &scenarioStats{
			Name:        scenario.Name,
			Description: scenario.Description,
			Category:    scenario.Category,
			Type:        scenario.Type,
			Latencies:   make([]float64, 0, 4096),
			mu:          sync.Mutex{},
		}
	}

	var (
		totalSuccess atomic.Int64
		sessionsUp   atomic.Int64
		sessionErrs  atomic.Int64
	)

	startTime := time.Now()

	for _, scenario := range scenarios {
		stats := scenarioStatsMap[scenario.Name]
		scenarioStart := time.Now()

		session, err := newCDPSession(benchCtx, b.logger.With("endpoint", wsURL, "scenario", scenario.Name), wsURL)
		if err != nil {
			sessionErrs.Add(1)
			stats.recordError(err)
			continue
		}

		if err := session.PrepareTarget(benchCtx); err != nil {
			sessionErrs.Add(1)
			stats.recordError(err)
			session.Close()
			continue
		}
		sessionsUp.Add(1)
		session.resetEvents()

		effectiveDuration := scenario.Duration
		if effectiveDuration == 0 && scenario.Iterations == 0 {
			effectiveDuration = 3 * time.Second
		}
		iterDeadline := time.Now().Add(effectiveDuration)
		iterations := scenario.Iterations
		for {
			select {
			case <-benchCtx.Done():
				iterDeadline = time.Now() // ensure exit
			default:
			}

			if effectiveDuration > 0 && time.Now().After(iterDeadline) {
				break
			}
			if scenario.Iterations > 0 && iterations <= 0 {
				break
			}

			runCtx := benchCtx
			cancelRun := func() {}
			if scenario.Timeout > 0 {
				runCtx, cancelRun = context.WithTimeout(benchCtx, scenario.Timeout)
			}

			start := time.Now()
			err = scenario.Run(runCtx, session)
			cancelRun()
			latency := time.Since(start)

			stats.Attempts.Add(1)

			if err != nil {
				stats.Failures.Add(1)
				stats.recordError(err)
			} else {
				totalSuccess.Add(1)
				stats.Successes.Add(1)
				stats.recordLatency(float64(latency.Microseconds()) / 1000.0)
			}

			if scenario.Iterations > 0 {
				iterations--
			}
		}

		stats.addDuration(time.Since(scenarioStart))
		stats.EventCount.Add(session.EventCount())
		session.Close()
	}

	elapsed := time.Since(startTime)
	totalSuccessCount := totalSuccess.Load()

	b.logger.Info("CDP benchmark completed", "duration", elapsed, "successful_ops", totalSuccessCount)

	totalThroughput := float64(totalSuccessCount) / elapsed.Seconds()

	scenarioResults := make([]CDPScenarioResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		stats := scenarioStatsMap[scenario.Name]
		attempts := stats.Attempts.Load()
		successes := stats.Successes.Load()
		failures := stats.Failures.Load()

		successRate := 0.0
		if attempts > 0 {
			successRate = (float64(successes) / float64(attempts)) * 100.0
		}

		durationSec := stats.durationSeconds()
		throughput := 0.0
		if durationSec > 0 {
			throughput = float64(successes) / durationSec
		}
		latencyMetrics := calculatePercentiles(stats.Latencies)

		scenarioResults = append(scenarioResults, CDPScenarioResult{
			Name:                scenario.Name,
			Description:         scenario.Description,
			Category:            scenario.Category,
			AttemptCount:        attempts,
			OperationCount:      successes,
			FailureCount:        failures,
			ThroughputOpsPerSec: throughput,
			EventCount:          stats.EventCount.Load(),
			EventThroughputSec:  float64(stats.EventCount.Load()) / durationSec,
			LatencyMS:           latencyMetrics,
			SuccessRate:         successRate,
			ErrorSamples:        stats.copyErrors(),
			DurationSeconds:     durationSec,
			Type:                scenario.Type,
		})
	}

	return &CDPEndpointResults{
		EndpointURL:              "",
		TotalThroughputOpsPerSec: totalThroughput,
		SessionsStarted:          int(sessionsUp.Load()),
		SessionFailures:          int(sessionErrs.Load()),
		Scenarios:                scenarioResults,
	}
}

// benchmarkScenarios defines deterministic CDP scenarios that require valid CDP sessions.
func benchmarkScenarios() []cdpScenario {
	quickDuration := 5 * time.Second
	quickTimeout := 3 * time.Second
	navTimeout := 15 * time.Second
	trendingTimeout := 18 * time.Second
	pageWarmupTimeout := 5 * time.Second

	return []cdpScenario{
		{
			Name:        "Runtime.evaluate-basic",
			Category:    "Runtime",
			Description: "Evaluate a simple arithmetic expression",
			Type:        "micro",
			Duration:    quickDuration,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    "21*2",
					"returnByValue": true,
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					Result struct {
						Value float64 `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if result.Result.Value != 42 {
					return fmt.Errorf("unexpected value: %v", result.Result.Value)
				}
				return nil
			},
		},
		{
			Name:        "Runtime.evaluate-dom",
			Category:    "Runtime",
			Description: "Evaluate JavaScript that reads DOM content",
			Type:        "micro",
			Duration:    quickDuration,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    "document.querySelector('#benchmark-root').dataset.value",
					"returnByValue": true,
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					Result struct {
						Value string `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if strings.TrimSpace(result.Result.Value) != "42" {
					return fmt.Errorf("unexpected dom value: %q", result.Result.Value)
				}
				return nil
			},
		},
		{
			Name:        "DOM.querySelector",
			Category:    "DOM",
			Description: "Query DOM for benchmark node",
			Type:        "dom",
			Duration:    quickDuration,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				rootID, err := session.ensureDocumentRoot(ctx)
				if err != nil {
					return err
				}

				resp, err := session.send(ctx, "DOM.querySelector", map[string]interface{}{
					"nodeId":   rootID,
					"selector": "#benchmark-root",
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					NodeID int64 `json:"nodeId"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if result.NodeID == 0 {
					return fmt.Errorf("empty nodeId from DOM.querySelector")
				}
				return nil
			},
		},
		{
			Name:        "DOM.getBoxModel",
			Category:    "DOM",
			Description: "Fetch layout information for benchmark node",
			Type:        "dom",
			Duration:    quickDuration,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				nodeID, err := session.benchmarkNodeID(ctx)
				if err != nil {
					return err
				}

				_, err = session.send(ctx, "DOM.getBoxModel", map[string]interface{}{
					"nodeId": nodeID,
				}, true)
				return err
			},
		},
		{
			Name:        "Performance.getMetrics",
			Category:    "Performance",
			Description: "Collect performance metrics from the page",
			Type:        "perf",
			Duration:    5 * time.Second,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				resp, err := session.send(ctx, "Performance.getMetrics", nil, true)
				if err != nil {
					return err
				}

				var result struct {
					Metrics []map[string]interface{} `json:"metrics"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if len(result.Metrics) == 0 {
					return fmt.Errorf("no metrics returned from Performance.getMetrics")
				}
				return nil
			},
		},
		{
			Name:        "Runtime.increment-counter",
			Category:    "Runtime",
			Description: "Mutate page state deterministically",
			Type:        "micro",
			Duration:    quickDuration,
			Timeout:     quickTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    "window.bumpCounter()",
					"returnByValue": true,
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					Result struct {
						Value float64 `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if result.Result.Value < 1 {
					return fmt.Errorf("counter did not increase: %v", result.Result.Value)
				}
				return nil
			},
		},
		{
			Name:        "Navigation.hackernews",
			Category:    "Navigation",
			Description: "Navigate to Hacker News and count headlines",
			Type:        "navigation",
			Iterations:  2,
			Timeout:     navTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				if err := session.navigateToURL(ctx, "https://news.ycombinator.com/"); err != nil {
					return err
				}
				if err := session.waitForReadyWithTimeout(ctx, pageWarmupTimeout); err != nil {
					return err
				}
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    "document.querySelectorAll('a.storylink, span.titleline a').length",
					"returnByValue": true,
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					Result struct {
						Value float64 `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if result.Result.Value < 20 {
					return fmt.Errorf("too few headlines found: %v", result.Result.Value)
				}
				return nil
			},
		},
		{
			Name:        "Navigation.github-trending",
			Category:    "Navigation",
			Description: "Navigate to GitHub trending and inspect repository list",
			Type:        "navigation",
			Iterations:  2,
			Timeout:     trendingTimeout,
			Run: func(ctx context.Context, session *cdpSession) error {
				if err := session.navigateToURL(ctx, "https://github.com/trending?since=daily"); err != nil {
					return err
				}
				if err := session.waitForReadyWithTimeout(ctx, pageWarmupTimeout); err != nil {
					return err
				}
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    "document.querySelectorAll('article.Box-row').length",
					"returnByValue": true,
				}, true)
				if err != nil {
					return err
				}

				var result struct {
					Result struct {
						Value float64 `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				if result.Result.Value < 5 {
					return fmt.Errorf("too few trending repos found: %v", result.Result.Value)
				}
				return nil
			},
		},
		{
			Name:        "Network.fetch-burst",
			Category:    "Network",
			Description: "Generate network traffic via fetch burst against data URLs",
			Type:        "network",
			Duration:    5 * time.Second,
			Timeout:     4 * time.Second,
			Run: func(ctx context.Context, session *cdpSession) error {
				if err := session.enableNetwork(ctx); err != nil {
					return err
				}
				resp, err := session.send(ctx, "Runtime.evaluate", map[string]interface{}{
					"expression":    `(async()=>{const payload='data:text/plain,'+('x'.repeat(256));const urls=new Array(5).fill(payload);const res=await Promise.all(urls.map(u=>fetch(u).then(r=>r.text())));return res.length;})()`,
					"returnByValue": true,
					"awaitPromise":  true,
				}, true)
				if err != nil {
					return err
				}
				var result struct {
					Result struct {
						Value interface{} `json:"value"`
					} `json:"result"`
				}
				if err := decodeCDPResult(resp.Result, &result); err != nil {
					return err
				}
				switch v := result.Result.Value.(type) {
				case float64:
					if v != 5 {
						return fmt.Errorf("unexpected fetch burst result: %v", v)
					}
				case int:
					if v != 5 {
						return fmt.Errorf("unexpected fetch burst result: %v", v)
					}
				default:
					return fmt.Errorf("unexpected fetch burst result type: %T", v)
				}
				return nil
			},
		},
	}
}

func (s *scenarioStats) recordLatency(latency float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Latencies = append(s.Latencies, latency)
}

func (s *scenarioStats) recordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ErrorSamples) >= maxErrorSamples {
		return
	}
	s.ErrorSamples = append(s.ErrorSamples, err.Error())
}

func (s *scenarioStats) copyErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.ErrorSamples))
	copy(out, s.ErrorSamples)
	return out
}

func (s *scenarioStats) addDuration(d time.Duration) {
	s.DurationNS.Add(d.Nanoseconds())
}

func (s *scenarioStats) durationSeconds() float64 {
	return float64(s.DurationNS.Load()) / float64(time.Second)
}

// cdpSession represents a single connection + target scoped to one worker.
type cdpSession struct {
	logger    *slog.Logger
	conn      *websocket.Conn
	sessionID string
	targetID  string
	rootID    int64
	msgID     atomic.Int64
	events    atomic.Int64
}

func newCDPSession(ctx context.Context, logger *slog.Logger, wsURL string) (*cdpSession, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open WebSocket: %w", err)
	}
	// Allow larger CDP messages (events, responses)
	conn.SetReadLimit(10 * 1024 * 1024)

	return &cdpSession{
		logger: logger,
		conn:   conn,
		msgID:  atomic.Int64{},
	}, nil
}

func (s *cdpSession) Close() {
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s.targetID != "" {
		_, _ = s.send(closeCtx, "Target.closeTarget", map[string]interface{}{
			"targetId": s.targetID,
		}, false)
	}
	if s.sessionID != "" {
		_, _ = s.send(closeCtx, "Target.detachFromTarget", map[string]interface{}{
			"sessionId": s.sessionID,
		}, false)
	}
	// Close any remaining open targets belonging to this session to avoid tab leaks
	_, _ = s.send(closeCtx, "Target.getTargets", nil, false)
	s.conn.Close(websocket.StatusNormalClosure, "benchmark-complete")
}

// PrepareTarget creates and attaches to a dedicated target with a deterministic page.
func (s *cdpSession) PrepareTarget(ctx context.Context) error {
	createResp, err := s.send(ctx, "Target.createTarget", map[string]interface{}{
		"url": fmt.Sprintf("data:text/html,%s", url.PathEscape(benchmarkPageHTML)),
	}, false)
	if err != nil {
		return fmt.Errorf("create target: %w", err)
	}

	var createResult struct {
		TargetID string `json:"targetId"`
	}
	if err := decodeCDPResult(createResp.Result, &createResult); err != nil {
		return fmt.Errorf("decode create target: %w", err)
	}
	if createResult.TargetID == "" {
		return fmt.Errorf("empty targetId from createTarget")
	}
	s.targetID = createResult.TargetID

	attachResp, err := s.send(ctx, "Target.attachToTarget", map[string]interface{}{
		"targetId": createResult.TargetID,
		"flatten":  true,
	}, false)
	if err != nil {
		return fmt.Errorf("attach to target: %w", err)
	}

	var attachResult struct {
		SessionID string `json:"sessionId"`
	}
	if err := decodeCDPResult(attachResp.Result, &attachResult); err != nil {
		return fmt.Errorf("decode attachToTarget: %w", err)
	}
	if attachResult.SessionID == "" {
		return fmt.Errorf("empty sessionId from attachToTarget")
	}
	s.sessionID = attachResult.SessionID

	if err := s.enableDomains(ctx); err != nil {
		return err
	}
	if err := s.navigateToBenchmarkPage(ctx); err != nil {
		return err
	}
	if _, err := s.ensureDocumentRoot(ctx); err != nil {
		return err
	}
	return nil
}

func (s *cdpSession) enableDomains(ctx context.Context) error {
	domains := []string{"Page.enable", "Runtime.enable", "DOM.enable", "Performance.enable"}
	for _, method := range domains {
		if _, err := s.send(ctx, method, nil, true); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
	return nil
}

func (s *cdpSession) enableNetwork(ctx context.Context) error {
	if _, err := s.send(ctx, "Network.enable", nil, true); err != nil {
		return fmt.Errorf("Network.enable: %w", err)
	}
	return nil
}

func (s *cdpSession) resetEvents() {
	s.events.Store(0)
}

func (s *cdpSession) navigateToBenchmarkPage(ctx context.Context) error {
	s.rootID = 0
	if _, err := s.send(ctx, "Page.navigate", map[string]interface{}{
		"url": fmt.Sprintf("data:text/html,%s", url.PathEscape(benchmarkPageHTML)),
	}, true); err != nil {
		return fmt.Errorf("navigate benchmark: %w", err)
	}
	return s.waitForReady(ctx)
}

func (s *cdpSession) navigateToURL(ctx context.Context, targetURL string) error {
	s.rootID = 0
	if _, err := s.send(ctx, "Page.navigate", map[string]interface{}{
		"url": targetURL,
	}, true); err != nil {
		return fmt.Errorf("navigate %s: %w", targetURL, err)
	}
	return s.waitForReady(ctx)
}

func (s *cdpSession) waitForReady(ctx context.Context) error {
	return s.waitForReadyWithTimeout(ctx, 0)
}

func (s *cdpSession) waitForReadyWithTimeout(ctx context.Context, override time.Duration) error {
	if override > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, override)
		defer cancel()
	}

	for i := 0; i < 40; i++ {
		resp, err := s.send(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    "document.readyState",
			"returnByValue": true,
		}, true)
		if err != nil {
			return fmt.Errorf("readyState: %w", err)
		}
		var result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := decodeCDPResult(resp.Result, &result); err != nil {
			return err
		}
		if result.Result.Value == "complete" {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("page did not reach readyState complete")
}

func (s *cdpSession) ensureDocumentRoot(ctx context.Context) (int64, error) {
	if s.rootID != 0 {
		return s.rootID, nil
	}
	resp, err := s.send(ctx, "DOM.getDocument", map[string]interface{}{
		"depth": 1,
	}, true)
	if err != nil {
		return 0, fmt.Errorf("DOM.getDocument: %w", err)
	}
	var result struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := decodeCDPResult(resp.Result, &result); err != nil {
		return 0, err
	}
	if result.Root.NodeID == 0 {
		return 0, fmt.Errorf("DOM.getDocument returned empty root node")
	}
	s.rootID = result.Root.NodeID
	return s.rootID, nil
}

func (s *cdpSession) benchmarkNodeID(ctx context.Context) (int64, error) {
	rootID, err := s.ensureDocumentRoot(ctx)
	if err != nil {
		return 0, err
	}
	resp, err := s.send(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   rootID,
		"selector": "#benchmark-root",
	}, true)
	if err != nil {
		return 0, err
	}
	var result struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := decodeCDPResult(resp.Result, &result); err != nil {
		return 0, err
	}
	if result.NodeID == 0 {
		return 0, fmt.Errorf("DOM.querySelector returned empty node for benchmark-root")
	}
	return result.NodeID, nil
}

func (s *cdpSession) send(ctx context.Context, method string, params map[string]interface{}, useSession bool) (*CDPMessage, error) {
	id := int(s.msgID.Add(1))

	msg := CDPMessage{
		ID:     id,
		Method: method,
		Params: params,
	}
	if useSession {
		if s.sessionID == "" {
			return nil, fmt.Errorf("session not attached for %s", method)
		}
		msg.SessionID = s.sessionID
	}

	requestBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", method, err)
	}

	if err := s.conn.Write(ctx, websocket.MessageText, requestBytes); err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	for {
		_, responseBytes, err := s.conn.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", method, err)
		}

		var response CDPMessage
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}

		if response.ID == 0 || (response.ID != id && response.Method != "") {
			s.incrementEvents()
		}

		if response.ID != id {
			continue
		}

		if response.Error != nil {
			return &response, fmt.Errorf("%s failed: %s (code %d)", method, response.Error.Message, response.Error.Code)
		}

		return &response, nil
	}
}

// fetchBrowserWebSocketURL fetches the browser WebSocket debugger URL
func fetchBrowserWebSocketURL(baseURL string) (string, error) {
	if u, err := url.Parse(baseURL); err == nil && u.Scheme == "" {
		baseURL = "http://" + baseURL
	}

	jsonURL := baseURL + "/json/version"

	resp, err := http.Get(jsonURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", jsonURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, jsonURL, string(body))
	}

	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return "", fmt.Errorf("failed to decode JSON from %s: %w", jsonURL, err)
	}

	if versionInfo.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no webSocketDebuggerUrl in response from %s", jsonURL)
	}

	return versionInfo.WebSocketDebuggerURL, nil
}

// calculatePercentiles calculates latency percentiles
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
	ID        int                    `json:"id"`
	SessionID string                 `json:"sessionId,omitempty"`
	Method    string                 `json:"method,omitempty"`
	Params    map[string]interface{} `json:"params,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	Error     *CDPError              `json:"error,omitempty"`
}

func (s *cdpSession) EventCount() int64 {
	return s.events.Load()
}

func (s *cdpSession) incrementEvents() {
	s.events.Add(1)
}

// CDPError represents a CDP error response
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// decodeCDPResult safely decodes a CDP result payload into the provided struct.
func decodeCDPResult(result map[string]interface{}, v interface{}) error {
	if result == nil {
		return fmt.Errorf("missing result payload")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal result: %w", err)
	}
	return nil
}
