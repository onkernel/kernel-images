// Tool to reproduce Stop concurrency behavior: start a recording, trigger concurrent stops,
// then download and validate the resulting video with ffprobe.
// Usage: go run main.go -url http://localhost:10001 -duration 3 -concurrency 2
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	retry "github.com/avast/retry-go/v5"
	"github.com/nrednav/cuid2"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func main() {
	baseURL := flag.String("url", "http://localhost:444", "Base URL of the kernel-images API")
	duration := flag.Int("duration", 6, "Recording duration in seconds before stopping")
	concurrency := flag.Int("concurrency", 2, "Number of concurrent stop calls")
	iterations := flag.Int("iterations", 5, "Number of test iterations")
	skipWarmup := flag.Bool("skip-warmup", false, "Skip warmup recording (useful if display is already warmed up)")
	flag.Parse()

	fmt.Printf("Testing concurrent stop race condition\n")
	fmt.Printf("  URL: %s\n", *baseURL)
	fmt.Printf("  Duration: %ds\n", *duration)
	fmt.Printf("  Concurrency: %d\n", *concurrency)
	fmt.Printf("  Iterations: %d\n", *iterations)

	// Warmup: run a throwaway recording to ensure X11 display is ready for capture.
	// The first recording(s) after container start often fail because x11grab
	// can't get frames from the display while it's still initializing.
	if !*skipWarmup {
		fmt.Printf("\n=== Warmup (ensuring X11 display is ready) ===\n")
		if err := runWarmup(*baseURL); err != nil {
			fmt.Printf("  Warmup failed: %v (continuing anyway)\n", err)
		} else {
			fmt.Printf("  Warmup complete\n")
		}
		fmt.Println()
	}

	passed := 0
	failed := 0

	for i := 0; i < *iterations; i++ {
		testID := fmt.Sprintf("race-test-%s-%d", cuid2.Generate(), i)

		fmt.Printf("=== Iteration %d/%d (id=%s) ===\n", i+1, *iterations, testID)

		err := runTest(*baseURL, testID, *duration, *concurrency)
		if err != nil {
			fmt.Printf("❌ FAILED: %v\n\n", err)
			failed++
		} else {
			fmt.Printf("✅ PASSED\n\n")
			passed++
		}
	}

	fmt.Printf("=== RESULTS: %d passed, %d failed ===\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// runWarmup performs a throwaway recording to warm up the X11 display.
// This ensures subsequent recordings can capture frames reliably.
func runWarmup(baseURL string) error {
	ctx := context.Background()
	warmupID := fmt.Sprintf("warmup-%s", cuid2.Generate())

	client, err := oapi.NewClientWithResponses(baseURL)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Start warmup recording
	if err := startRecording(ctx, client, warmupID); err != nil {
		return fmt.Errorf("failed to start warmup recording: %w", err)
	}

	// Let it run for a few seconds to warm up the display
	time.Sleep(5 * time.Second)

	// Stop the warmup recording (single call, not concurrent)
	if err := stopRecording(ctx, client, warmupID); err != nil {
		// Ignore stop errors - we just need the warmup effect
	}

	// Clean up
	_ = deleteRecording(ctx, client, warmupID)

	return nil
}

func runTest(baseURL, replayID string, duration, concurrency int) error {
	ctx := context.Background()

	client, err := oapi.NewClientWithResponses(baseURL)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	fmt.Printf("  Starting recording...\n")
	if err := startRecording(ctx, client, replayID); err != nil {
		return fmt.Errorf("failed to start recording: %w", err)
	}

	fmt.Printf("  Recording for %d seconds...\n", duration)
	time.Sleep(time.Duration(duration) * time.Second)

	fmt.Printf("  Calling stop %d times concurrently...\n", concurrency)
	stopResults := make(chan error, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			err := stopRecording(ctx, client, replayID)
			if err != nil {
				stopResults <- fmt.Errorf("goroutine %d: %w", goroutineID, err)
			} else {
				stopResults <- nil
			}
		}(i)
	}

	wg.Wait()
	close(stopResults)

	var stopErrors []error
	for err := range stopResults {
		if err != nil {
			stopErrors = append(stopErrors, err)
		}
	}
	if len(stopErrors) > 0 {
		fmt.Printf("  Stop errors: %v\n", stopErrors)
	}

	fmt.Printf("  Downloading recording...\n")
	data, err := downloadRecording(ctx, client, replayID)
	if err != nil {
		return fmt.Errorf("failed to download recording: %w", err)
	}
	fmt.Printf("  Downloaded %d bytes\n", len(data))

	tmpFile, err := os.CreateTemp("", "race-test-*.mp4")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	fmt.Printf("  Validating with ffprobe...\n")
	if err := validateMP4(tmpFile.Name()); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Printf("  Cleaning up...\n")
	_ = deleteRecording(ctx, client, replayID)

	return nil
}

func startRecording(ctx context.Context, client *oapi.ClientWithResponses, replayID string) error {
	resp, err := client.StartRecordingWithResponse(ctx, oapi.StartRecordingJSONRequestBody{
		Id: &replayID,
	})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusConflict {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}

	return nil
}

func stopRecording(ctx context.Context, client *oapi.ClientWithResponses, replayID string) error {
	resp, err := client.StopRecordingWithResponse(ctx, oapi.StopRecordingJSONRequestBody{
		Id: &replayID,
	})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}

	return nil
}

func downloadRecording(ctx context.Context, client *oapi.ClientWithResponses, replayID string) ([]byte, error) {
	var data []byte
	err := retry.New(
		retry.Attempts(10),
		retry.Delay(500*time.Millisecond),
		retry.DelayType(retry.FixedDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
	).Do(func() error {
		resp, err := client.DownloadRecordingWithResponse(ctx, &oapi.DownloadRecordingParams{
			Id: &replayID,
		})
		if err != nil {
			return err
		}

		if resp.StatusCode() == http.StatusAccepted {
			return fmt.Errorf("recording not ready yet")
		}

		if resp.StatusCode() != http.StatusOK {
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
		}

		data = resp.Body
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed after retries: %w", err)
	}
	return data, nil
}

func deleteRecording(ctx context.Context, client *oapi.ClientWithResponses, replayID string) error {
	resp, err := client.DeleteRecordingWithResponse(ctx, oapi.DeleteRecordingJSONRequestBody{
		Id: &replayID,
	})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNotFound {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
	}

	return nil
}

func validateMP4(filePath string) error {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-output_format", "json",
		filePath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffprobe failed: %w\nOutput: %s", err, string(output))
	}

	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	if result.Format.Duration == "" {
		return fmt.Errorf("no duration found in video - file may be corrupt")
	}

	fmt.Printf("  Video duration: %s seconds\n", result.Format.Duration)
	return nil
}
