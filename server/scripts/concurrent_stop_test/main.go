// concurrent_stop_test tests the race condition when Stop is called concurrently.
//
// Usage:
//
//	go run main.go -url http://localhost:10001 -duration 3 -concurrency 2
//
// The test:
// 1. Starts a recording
// 2. Waits for the specified duration
// 3. Calls POST /recording/stop concurrently from multiple goroutines
// 4. Downloads the recording and validates it with ffprobe
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

	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

func main() {
	baseURL := flag.String("url", "http://localhost:444", "Base URL of the kernel-images API")
	duration := flag.Int("duration", 3, "Recording duration in seconds before stopping")
	concurrency := flag.Int("concurrency", 2, "Number of concurrent stop calls")
	iterations := flag.Int("iterations", 5, "Number of test iterations")
	replayID := flag.String("id", "", "Custom replay ID (default: auto-generated)")
	flag.Parse()

	fmt.Printf("Testing concurrent stop race condition\n")
	fmt.Printf("  URL: %s\n", *baseURL)
	fmt.Printf("  Duration: %ds\n", *duration)
	fmt.Printf("  Concurrency: %d\n", *concurrency)
	fmt.Printf("  Iterations: %d\n", *iterations)
	fmt.Println()

	passed := 0
	failed := 0

	for i := 0; i < *iterations; i++ {
		testID := *replayID
		if testID == "" {
			testID = fmt.Sprintf("race-test-%d-%d", time.Now().UnixNano(), i)
		}

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

func runTest(baseURL, replayID string, duration, concurrency int) error {
	ctx := context.Background()

	// Create oapi client
	client, err := oapi.NewClientWithResponses(baseURL)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// 1. Start recording
	fmt.Printf("  Starting recording...\n")
	if err := startRecording(ctx, client, replayID); err != nil {
		return fmt.Errorf("failed to start recording: %w", err)
	}

	// 2. Wait for recording to capture content
	fmt.Printf("  Recording for %d seconds...\n", duration)
	time.Sleep(time.Duration(duration) * time.Second)

	// 3. Call stop concurrently
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

	// Check stop results
	var stopErrors []error
	for err := range stopResults {
		if err != nil {
			stopErrors = append(stopErrors, err)
		}
	}
	if len(stopErrors) > 0 {
		fmt.Printf("  Stop errors: %v\n", stopErrors)
		// Don't fail yet - the recording might still be valid
	}

	// 4. Download recording
	fmt.Printf("  Downloading recording...\n")
	data, err := downloadRecording(ctx, client, replayID)
	if err != nil {
		return fmt.Errorf("failed to download recording: %w", err)
	}
	fmt.Printf("  Downloaded %d bytes\n", len(data))

	// 5. Save to temp file and validate with ffprobe
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

	// 6. Clean up - delete recording
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
	// Retry a few times since the recording might still be finalizing
	var lastErr error
	for i := 0; i < 10; i++ {
		resp, err := client.DownloadRecordingWithResponse(ctx, &oapi.DownloadRecordingParams{
			Id: &replayID,
		})
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if resp.StatusCode() == http.StatusAccepted {
			// Still processing, retry
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode() != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
			time.Sleep(500 * time.Millisecond)
			continue
		}

		return resp.Body, nil
	}

	return nil, fmt.Errorf("failed after retries: %w", lastErr)
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
	// Use ffprobe to validate the MP4 file
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

	// Parse the output to check for valid duration
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
