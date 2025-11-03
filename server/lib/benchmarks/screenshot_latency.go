package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ScreenshotLatencyBenchmark measures screenshot capture performance
type ScreenshotLatencyBenchmark struct {
	logger     *slog.Logger
	apiBaseURL string
}

// NewScreenshotLatencyBenchmark creates a new screenshot latency benchmark
func NewScreenshotLatencyBenchmark(logger *slog.Logger, apiBaseURL string) *ScreenshotLatencyBenchmark {
	return &ScreenshotLatencyBenchmark{
		logger:     logger,
		apiBaseURL: apiBaseURL,
	}
}

// ScreenshotLatencyResults contains screenshot benchmark results
type ScreenshotLatencyResults struct {
	TotalScreenshots     int            `json:"total_screenshots"`
	SuccessfulCaptures   int            `json:"successful_captures"`
	FailedCaptures       int            `json:"failed_captures"`
	SuccessRate          float64        `json:"success_rate"`
	LatencyMS            LatencyMetrics `json:"latency_ms"`
	AvgImageSizeBytes    int64          `json:"avg_image_size_bytes"`
	ThroughputPerSec     float64        `json:"throughput_per_sec"`
}

// Run executes the screenshot latency benchmark
// Takes exactly 5 screenshots with variations introduced via computer control API
func (b *ScreenshotLatencyBenchmark) Run(ctx context.Context, duration time.Duration) (*ScreenshotLatencyResults, error) {
	b.logger.Info("starting screenshot latency benchmark - 5 screenshots with variations")

	const numScreenshots = 5

	var (
		successfulCaptures int
		failedCaptures     int
		totalImageSize     int64
		latencies          []float64
	)

	startTime := time.Now()
	client := &http.Client{Timeout: 10 * time.Second}
	screenshotURL := fmt.Sprintf("%s/computer/screenshot", b.apiBaseURL)

	// Take 5 screenshots with variations between each
	for i := 0; i < numScreenshots; i++ {
		b.logger.Info("taking screenshot", "number", i+1)

		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, "POST", screenshotURL, nil)
		if err != nil {
			b.logger.Error("failed to create screenshot request", "err", err)
			failedCaptures++
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			b.logger.Error("screenshot request failed", "err", err)
			failedCaptures++
			continue
		}

		if resp.StatusCode != http.StatusOK {
			b.logger.Error("screenshot returned non-200 status", "status", resp.StatusCode)
			resp.Body.Close()
			failedCaptures++
			continue
		}

		// Read response body to measure actual image size
		imageData, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.logger.Error("failed to read screenshot response body", "err", err)
			failedCaptures++
			continue
		}

		imageSize := int64(len(imageData))
		latency := time.Since(start)
		successfulCaptures++
		totalImageSize += imageSize
		latencies = append(latencies, float64(latency.Milliseconds()))

		// Introduce variation between screenshots (except after the last one)
		if i < numScreenshots-1 {
			b.introduceVariation(ctx, client, i)
		}
	}

	elapsed := time.Since(startTime)

	// Calculate metrics
	totalScreenshots := successfulCaptures + failedCaptures
	successRate := 0.0
	if totalScreenshots > 0 {
		successRate = (float64(successfulCaptures) / float64(totalScreenshots)) * 100.0
	}

	avgImageSize := int64(0)
	if successfulCaptures > 0 {
		avgImageSize = totalImageSize / int64(successfulCaptures)
	}

	latencyMetrics := calculatePercentiles(latencies)
	throughput := float64(successfulCaptures) / elapsed.Seconds()

	b.logger.Info("screenshot latency benchmark completed",
		"total", totalScreenshots,
		"successful", successfulCaptures,
		"failed", failedCaptures,
		"success_rate", successRate,
		"avg_image_size_kb", avgImageSize/1024)

	return &ScreenshotLatencyResults{
		TotalScreenshots:   totalScreenshots,
		SuccessfulCaptures: successfulCaptures,
		FailedCaptures:     failedCaptures,
		SuccessRate:        successRate,
		LatencyMS:          latencyMetrics,
		AvgImageSizeBytes:  avgImageSize,
		ThroughputPerSec:   throughput,
	}, nil
}

// introduceVariation uses computer control APIs to create variations between screenshots
func (b *ScreenshotLatencyBenchmark) introduceVariation(ctx context.Context, client *http.Client, iteration int) {
	// Introduce different types of variations based on iteration
	// This creates different screen states for more realistic benchmark

	switch iteration % 4 {
	case 0:
		// Move mouse to different positions
		b.moveMouse(ctx, client, 400, 300)
		time.Sleep(200 * time.Millisecond)

	case 1:
		// Scroll down
		b.scroll(ctx, client, 500, 400, 0, 3)
		time.Sleep(200 * time.Millisecond)

	case 2:
		// Click at a position (might interact with page elements)
		b.clickMouse(ctx, client, 600, 400)
		time.Sleep(200 * time.Millisecond)

	case 3:
		// Scroll back up
		b.scroll(ctx, client, 500, 400, 0, -3)
		time.Sleep(200 * time.Millisecond)
	}
}

// moveMouse moves mouse to specified coordinates
func (b *ScreenshotLatencyBenchmark) moveMouse(ctx context.Context, client *http.Client, x, y int) {
	payload := map[string]int{"x": x, "y": y}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/computer/move_mouse", b.apiBaseURL),
		bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// clickMouse clicks at specified coordinates
func (b *ScreenshotLatencyBenchmark) clickMouse(ctx context.Context, client *http.Client, x, y int) {
	payload := map[string]interface{}{
		"x":          x,
		"y":          y,
		"button":     "left",
		"click_type": "click",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/computer/click_mouse", b.apiBaseURL),
		bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// scroll scrolls at specified coordinates
func (b *ScreenshotLatencyBenchmark) scroll(ctx context.Context, client *http.Client, x, y, deltaX, deltaY int) {
	payload := map[string]int{
		"x":       x,
		"y":       y,
		"delta_x": deltaX,
		"delta_y": deltaY,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/computer/scroll", b.apiBaseURL),
		bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
