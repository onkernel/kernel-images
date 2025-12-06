//go:build linux

package benchmarks

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// GetSystemMemoryTotalMB returns total system memory (host or container limit) in MB.
func GetSystemMemoryTotalMB() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/meminfo: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseFloat(fields[1], 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse MemTotal: %w", err)
				}
				return kb / 1024.0, nil // KB -> MB
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to scan /proc/meminfo: %w", err)
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}
