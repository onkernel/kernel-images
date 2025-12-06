package benchmarks

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// StartupPhase represents a phase of server initialization
type StartupPhase struct {
	Name      string        `json:"name"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration_ms"`
}

// StartupTiming tracks server initialization phases
type StartupTiming struct {
	mu               sync.RWMutex
	serverStartTime  time.Time
	phases           []StartupPhase
	currentPhase     *StartupPhase
	totalStartupTime time.Duration
}

// Global startup timing instance
var globalStartupTiming = &StartupTiming{
	serverStartTime: time.Now(),
	phases:          make([]StartupPhase, 0, 16),
}

// GetGlobalStartupTiming returns the global startup timing tracker
func GetGlobalStartupTiming() *StartupTiming {
	return globalStartupTiming
}

// StartPhase begins timing a new startup phase
func (st *StartupTiming) StartPhase(name string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// End previous phase if exists
	if st.currentPhase != nil {
		st.currentPhase.EndTime = time.Now()
		st.currentPhase.Duration = st.currentPhase.EndTime.Sub(st.currentPhase.StartTime)
		st.phases = append(st.phases, *st.currentPhase)
	}

	// Start new phase
	st.currentPhase = &StartupPhase{
		Name:      name,
		StartTime: time.Now(),
	}
}

// EndPhase ends the current phase
func (st *StartupTiming) EndPhase() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.currentPhase != nil {
		st.currentPhase.EndTime = time.Now()
		st.currentPhase.Duration = st.currentPhase.EndTime.Sub(st.currentPhase.StartTime)
		st.phases = append(st.phases, *st.currentPhase)
		st.currentPhase = nil
	}
}

// MarkServerReady marks the server as fully initialized
func (st *StartupTiming) MarkServerReady() {
	st.mu.Lock()
	defer st.mu.Unlock()

	// End current phase if exists
	if st.currentPhase != nil {
		st.currentPhase.EndTime = time.Now()
		st.currentPhase.Duration = st.currentPhase.EndTime.Sub(st.currentPhase.StartTime)
		st.phases = append(st.phases, *st.currentPhase)
		st.currentPhase = nil
	}

	st.totalStartupTime = time.Since(st.serverStartTime)
}

// GetPhases returns all recorded startup phases
func (st *StartupTiming) GetPhases() []StartupPhase {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// Make a copy
	phases := make([]StartupPhase, len(st.phases))
	copy(phases, st.phases)
	return phases
}

// GetTotalStartupTime returns the total time from server start to ready
func (st *StartupTiming) GetTotalStartupTime() time.Duration {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.totalStartupTime
}

// StartupTimingResults contains startup timing data for benchmark results
type StartupTimingResults struct {
	TotalStartupTimeMS float64       `json:"total_startup_time_ms"`
	Phases             []PhaseResult `json:"phases"`
	PhaseSummary       PhaseSummary  `json:"phase_summary"`
}

type PhaseResult struct {
	Name       string  `json:"name"`
	DurationMS float64 `json:"duration_ms"`
	Percentage float64 `json:"percentage"`
}

type PhaseSummary struct {
	FastestPhase string  `json:"fastest_phase"`
	SlowestPhase string  `json:"slowest_phase"`
	FastestMS    float64 `json:"fastest_ms"`
	SlowestMS    float64 `json:"slowest_ms"`
}

// GetContainerStartupTiming reads startup timing from the wrapper.sh export file
func GetContainerStartupTiming() (*StartupTimingResults, error) {
	const timingFile = "/tmp/kernel_startup_timing.json"

	// Check if file exists
	if _, err := os.Stat(timingFile); os.IsNotExist(err) {
		// File doesn't exist yet - return nil
		return nil, nil
	}

	// Read and parse the file
	data, err := os.ReadFile(timingFile)
	if err != nil {
		return nil, err
	}

	var containerTiming struct {
		TotalStartupTimeMS float64 `json:"total_startup_time_ms"`
		Phases             []struct {
			Name       string  `json:"name"`
			DurationMS float64 `json:"duration_ms"`
		} `json:"phases"`
	}

	if err := json.Unmarshal(data, &containerTiming); err != nil {
		return nil, err
	}

	// Convert to our format
	results := &StartupTimingResults{
		TotalStartupTimeMS: containerTiming.TotalStartupTimeMS,
		Phases:             make([]PhaseResult, len(containerTiming.Phases)),
	}

	var fastestIdx, slowestIdx int
	if len(containerTiming.Phases) > 0 {
		fastestDur := containerTiming.Phases[0].DurationMS
		slowestDur := containerTiming.Phases[0].DurationMS

		for i, phase := range containerTiming.Phases {
			total := containerTiming.TotalStartupTimeMS
			if total <= 0 {
				total = 0
			}
			percentage := 0.0
			if total > 0 {
				percentage = (phase.DurationMS / total) * 100.0
			}

			results.Phases[i] = PhaseResult{
				Name:       phase.Name,
				DurationMS: phase.DurationMS,
				Percentage: percentage,
			}

			if phase.DurationMS < fastestDur {
				fastestDur = phase.DurationMS
				fastestIdx = i
			}
			if phase.DurationMS > slowestDur {
				slowestDur = phase.DurationMS
				slowestIdx = i
			}
		}

		results.PhaseSummary = PhaseSummary{
			FastestPhase: containerTiming.Phases[fastestIdx].Name,
			SlowestPhase: containerTiming.Phases[slowestIdx].Name,
			FastestMS:    fastestDur,
			SlowestMS:    slowestDur,
		}
	}

	return results, nil
}

// GetStartupTimingResults converts startup timing to benchmark results format
func GetStartupTimingResults() *StartupTimingResults {
	st := GetGlobalStartupTiming()
	phases := st.GetPhases()
	totalTime := st.GetTotalStartupTime()

	if totalTime == 0 || len(phases) == 0 {
		return &StartupTimingResults{
			TotalStartupTimeMS: 0,
			Phases:             []PhaseResult{},
			PhaseSummary:       PhaseSummary{},
		}
	}

	results := &StartupTimingResults{
		TotalStartupTimeMS: float64(totalTime.Milliseconds()),
		Phases:             make([]PhaseResult, len(phases)),
	}

	var fastestIdx, slowestIdx int
	fastestDur := phases[0].Duration
	slowestDur := phases[0].Duration

	for i, phase := range phases {
		durationMS := float64(phase.Duration.Milliseconds())
		percentage := (float64(phase.Duration) / float64(totalTime)) * 100.0

		results.Phases[i] = PhaseResult{
			Name:       phase.Name,
			DurationMS: durationMS,
			Percentage: percentage,
		}

		if phase.Duration < fastestDur {
			fastestDur = phase.Duration
			fastestIdx = i
		}
		if phase.Duration > slowestDur {
			slowestDur = phase.Duration
			slowestIdx = i
		}
	}

	results.PhaseSummary = PhaseSummary{
		FastestPhase: phases[fastestIdx].Name,
		SlowestPhase: phases[slowestIdx].Name,
		FastestMS:    float64(fastestDur.Milliseconds()),
		SlowestMS:    float64(slowestDur.Milliseconds()),
	}

	return results
}
