//go:build linux

package benchmarks

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const clockTicksPerSecond = 100.0 // Linux HZ is overwhelmingly 100 on contemporary distros

// CPUStats represents CPU usage statistics
type CPUStats struct {
	User   uint64
	System uint64
	Total  uint64
	// Timestamp records when the snapshot was taken so we can compute wall time deltas.
	Timestamp time.Time
}

// GetProcessCPUStats retrieves CPU stats for the current process
func GetProcessCPUStats() (*CPUStats, error) {
	now := time.Now()
	// Read /proc/self/stat
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/self/stat: %w", err)
	}

	// Parse the stat file
	// Fields: pid comm state ... utime stime ...
	// utime is field 14 (index 13), stime is field 15 (index 14)
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return nil, fmt.Errorf("unexpected /proc/self/stat format")
	}

	utime, err := strconv.ParseUint(fields[13], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse utime: %w", err)
	}

	stime, err := strconv.ParseUint(fields[14], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stime: %w", err)
	}

	return &CPUStats{
		User:   utime,
		System: stime,
		Total:  utime + stime,
		// Use the same timestamp for the snapshot so we can compute wall-clock deltas later.
		Timestamp: now,
	}, nil
}

// GetSystemCPUStats retrieves system-wide CPU stats
func GetSystemCPUStats() (*CPUStats, error) {
	now := time.Now()
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read /proc/stat")
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return nil, fmt.Errorf("unexpected /proc/stat format")
	}

	// cpu  user nice system idle iowait irq softirq ...
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil, fmt.Errorf("not enough fields in /proc/stat")
	}

	user, _ := strconv.ParseUint(fields[1], 10, 64)
	nice, _ := strconv.ParseUint(fields[2], 10, 64)
	system, _ := strconv.ParseUint(fields[3], 10, 64)
	idle, _ := strconv.ParseUint(fields[4], 10, 64)

	total := user + nice + system + idle
	if len(fields) >= 8 {
		iowait, _ := strconv.ParseUint(fields[5], 10, 64)
		irq, _ := strconv.ParseUint(fields[6], 10, 64)
		softirq, _ := strconv.ParseUint(fields[7], 10, 64)
		total += iowait + irq + softirq
	}

	return &CPUStats{
		User:      user + nice,
		System:    system,
		Total:     total,
		Timestamp: now,
	}, nil
}

// CalculateCPUPercent calculates CPU usage percentage from two snapshots
func CalculateCPUPercent(before, after *CPUStats) float64 {
	if before == nil || after == nil {
		return 0.0
	}

	deltaTotal := after.Total - before.Total
	if deltaTotal == 0 {
		return 0.0
	}

	elapsed := after.Timestamp.Sub(before.Timestamp).Seconds()
	if elapsed <= 0 {
		return 0.0
	}

	// Convert process clock ticks to seconds, then to percentage of wall time.
	procSeconds := float64(deltaTotal) / clockTicksPerSecond
	return (procSeconds / elapsed) * 100.0
}

// GetProcessMemoryMB returns the current memory usage of the process in MB (heap)
func GetProcessMemoryMB() float64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0.0
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmSize:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseFloat(fields[1], 64); err == nil {
					return kb / 1024.0 // Convert KB to MB
				}
			}
		}
	}
	return 0.0
}

// GetProcessRSSMemoryMB returns the RSS (Resident Set Size) memory usage in MB
func GetProcessRSSMemoryMB() (float64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0.0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseFloat(fields[1], 64); err == nil {
					return kb / 1024.0, nil // Convert KB to MB
				}
			}
		}
	}
	return 0.0, fmt.Errorf("VmRSS not found in /proc/self/status")
}
