package api

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/onkernel/kernel-images/server/lib/benchmarks"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// RunBenchmark implements the benchmark endpoint
// Each benchmark component runs for its own fixed duration and reports actual elapsed time
func (s *ApiService) RunBenchmark(ctx context.Context, request oapi.RunBenchmarkRequestObject) (oapi.RunBenchmarkResponseObject, error) {
	log := logger.FromContext(ctx)
	log.Info("starting benchmark run")

	// Parse parameters
	components := parseComponents(request.Params.Components)

	// Initialize results (duration will be calculated from actual elapsed time)
	startTime := time.Now()
	results := &benchmarks.BenchmarkResults{
		Timestamp: startTime,
		System:    getSystemInfo(),
		Results:   benchmarks.ComponentResults{},
		Errors:    []string{},
	}

	// Run requested benchmarks (each uses its own internal fixed duration)
	for _, component := range components {
		switch component {
		case benchmarks.ComponentCDP:
			if cdpResults, err := s.runCDPBenchmark(ctx); err != nil {
				log.Error("CDP benchmark failed", "err", err)
				results.Errors = append(results.Errors, fmt.Sprintf("CDP: %v", err))
			} else {
				results.Results.CDP = cdpResults
			}

		case benchmarks.ComponentWebRTC:
			if webrtcResults, err := s.runWebRTCBenchmark(ctx); err != nil {
				log.Error("WebRTC benchmark failed", "err", err)
				results.Errors = append(results.Errors, fmt.Sprintf("WebRTC: %v", err))
			} else {
				results.Results.WebRTCLiveView = webrtcResults
			}

		case benchmarks.ComponentRecording:
			if recordingResults, err := s.runRecordingBenchmark(ctx); err != nil {
				log.Error("Recording benchmark failed", "err", err)
				results.Errors = append(results.Errors, fmt.Sprintf("Recording: %v", err))
			} else {
				results.Results.Recording = recordingResults
			}
		}
	}

	// Calculate actual elapsed time
	elapsed := time.Since(startTime)
	results.ElapsedSeconds = elapsed.Seconds()

	log.Info("benchmark run completed", "elapsed_seconds", results.ElapsedSeconds)

	// Add container startup timing if available
	if containerTiming, err := benchmarks.GetContainerStartupTiming(); err == nil && containerTiming != nil {
		results.StartupTiming = containerTiming
	}

	// Convert to oapi response type
	return oapi.RunBenchmark200JSONResponse(convertToOAPIBenchmarkResults(results)), nil
}

// convertToOAPIBenchmarkResults converts benchmarks.BenchmarkResults to oapi.BenchmarkResults
func convertToOAPIBenchmarkResults(results *benchmarks.BenchmarkResults) oapi.BenchmarkResults {
	elapsedSecs := float32(results.ElapsedSeconds)
	resp := oapi.BenchmarkResults{
		Timestamp:      &results.Timestamp,
		ElapsedSeconds: &elapsedSecs,
		System:         convertSystemInfo(results.System),
		Results:        convertComponentResults(results.Results),
		Errors:         &results.Errors,
	}

	if results.StartupTiming != nil {
		resp.StartupTiming = convertStartupTimingResults(results.StartupTiming)
	}

	return resp
}

func convertSystemInfo(info benchmarks.SystemInfo) *oapi.SystemInfo {
	memTotal := int(info.MemoryTotalMB)
	return &oapi.SystemInfo{
		Arch:          &info.Arch,
		CpuCount:      &info.CPUCount,
		MemoryTotalMb: &memTotal,
		Os:            &info.OS,
	}
}

func convertComponentResults(results benchmarks.ComponentResults) *oapi.ComponentResults {
	resp := &oapi.ComponentResults{}

	if results.CDP != nil {
		resp.Cdp = convertCDPProxyResults(results.CDP)
	}

	if results.WebRTCLiveView != nil {
		resp.WebrtcLiveView = convertWebRTCResults(results.WebRTCLiveView)
	}

	if results.Recording != nil {
		resp.Recording = convertRecordingResults(results.Recording)
	}

	return resp
}

func convertCDPProxyResults(cdp *benchmarks.CDPProxyResults) *oapi.CDPProxyResults {
	proxyOverhead := float32(cdp.ProxyOverheadPercent)
	result := &oapi.CDPProxyResults{
		ConcurrentConnections: &cdp.ConcurrentConnections,
		MemoryMb:              convertMemoryMetrics(cdp.MemoryMB),
		ProxyOverheadPercent:  &proxyOverhead,
	}

	// Convert proxied endpoint results
	if cdp.ProxiedEndpoint != nil {
		result.ProxiedEndpoint = convertCDPEndpointResults(cdp.ProxiedEndpoint)
	}

	// Convert direct endpoint results
	if cdp.DirectEndpoint != nil {
		result.DirectEndpoint = convertCDPEndpointResults(cdp.DirectEndpoint)
	}

	return result
}

func convertCDPEndpointResults(endpoint *benchmarks.CDPEndpointResults) *oapi.CDPEndpointResults {
	throughput := float32(endpoint.TotalThroughputOpsPerSec)
	result := &oapi.CDPEndpointResults{
		EndpointUrl:              endpoint.EndpointURL,
		TotalThroughputOpsPerSec: throughput,
	}

	if endpoint.SessionsStarted > 0 {
		result.SessionsStarted = &endpoint.SessionsStarted
	}
	if endpoint.SessionFailures > 0 {
		result.SessionFailures = &endpoint.SessionFailures
	}

	// Convert scenarios
	scenarios := make([]oapi.CDPScenarioResult, len(endpoint.Scenarios))
	for i, scenario := range endpoint.Scenarios {
		opCount := int(scenario.OperationCount)
		attemptCount := int(scenario.AttemptCount)
		durationSec := float32(scenario.DurationSeconds)
		throughputOps := float32(scenario.ThroughputOpsPerSec)
		successRate := float32(scenario.SuccessRate)
		scenarios[i] = oapi.CDPScenarioResult{
			Name:                &scenario.Name,
			Description:         &scenario.Description,
			Category:            &scenario.Category,
			Type:                optionalString(scenario.Type),
			AttemptCount:        &attemptCount,
			OperationCount:      &opCount,
			DurationSeconds:     &durationSec,
			FailureCount:        optionalInt(scenario.FailureCount),
			ThroughputOpsPerSec: &throughputOps,
			LatencyMs:           convertLatencyMetrics(scenario.LatencyMS),
			SuccessRate:         &successRate,
		}
		if scenario.EventCount > 0 {
			events := int(scenario.EventCount)
			scenarios[i].EventCount = &events
			eventThroughput := float32(scenario.EventThroughputSec)
			scenarios[i].EventThroughputSec = &eventThroughput
		}
		if len(scenario.ErrorSamples) > 0 {
			samples := scenario.ErrorSamples
			scenarios[i].ErrorSamples = &samples
		}
	}
	result.Scenarios = scenarios

	return result
}

func convertWebRTCResults(webrtc *benchmarks.WebRTCLiveViewResults) *oapi.WebRTCLiveViewResults {
	cpuPct := float32(webrtc.CPUUsagePercent)
	return &oapi.WebRTCLiveViewResults{
		ConnectionState:    &webrtc.ConnectionState,
		IceConnectionState: &webrtc.IceConnectionState,
		FrameRateFps:       convertFrameRateMetrics(webrtc.FrameRateFPS),
		FrameLatencyMs:     convertLatencyMetrics(webrtc.FrameLatencyMS),
		BitrateKbps:        convertBitrateMetrics(webrtc.BitrateKbps),
		Packets:            convertPacketMetrics(webrtc.Packets),
		Frames:             convertFrameMetrics(webrtc.Frames),
		JitterMs:           convertJitterMetrics(webrtc.JitterMS),
		Network:            convertNetworkMetrics(webrtc.Network),
		Codecs:             convertCodecMetrics(webrtc.Codecs),
		Resolution:         convertResolutionMetrics(webrtc.Resolution),
		ConcurrentViewers:  &webrtc.ConcurrentViewers,
		CpuUsagePercent:    &cpuPct,
		MemoryMb:           convertMemoryMetrics(webrtc.MemoryMB),
	}
}

func convertRecordingResults(rec *benchmarks.RecordingResults) *oapi.RecordingResults {
	cpuOverhead := float32(rec.CPUOverheadPercent)
	memOverhead := float32(rec.MemoryOverheadMB)
	framesCaptured := int(rec.FramesCaptured)
	framesDropped := int(rec.FramesDropped)
	encodingLag := float32(rec.AvgEncodingLagMS)
	diskWrite := float32(rec.DiskWriteMBPS)

	result := &oapi.RecordingResults{
		CpuOverheadPercent:   &cpuOverhead,
		MemoryOverheadMb:     &memOverhead,
		FramesCaptured:       &framesCaptured,
		FramesDropped:        &framesDropped,
		AvgEncodingLagMs:     &encodingLag,
		DiskWriteMbps:        &diskWrite,
		ConcurrentRecordings: &rec.ConcurrentRecordings,
	}

	if rec.FrameRateImpact != nil {
		beforeFPS := float32(rec.FrameRateImpact.BeforeRecordingFPS)
		duringFPS := float32(rec.FrameRateImpact.DuringRecordingFPS)
		impactPct := float32(rec.FrameRateImpact.ImpactPercent)
		result.FrameRateImpact = &oapi.RecordingFrameRateImpact{
			BeforeRecordingFps: &beforeFPS,
			DuringRecordingFps: &duringFPS,
			ImpactPercent:      &impactPct,
		}
	}

	return result
}

func convertLatencyMetrics(lat benchmarks.LatencyMetrics) *oapi.LatencyMetrics {
	p50 := float32(lat.P50)
	p95 := float32(lat.P95)
	p99 := float32(lat.P99)
	return &oapi.LatencyMetrics{
		P50: &p50,
		P95: &p95,
		P99: &p99,
	}
}

func convertFrameRateMetrics(fr benchmarks.FrameRateMetrics) *oapi.FrameRateMetrics {
	target := float32(fr.Target)
	achieved := float32(fr.Achieved)
	min := float32(fr.Min)
	max := float32(fr.Max)
	return &oapi.FrameRateMetrics{
		Target:   &target,
		Achieved: &achieved,
		Min:      &min,
		Max:      &max,
	}
}

func convertBitrateMetrics(br benchmarks.BitrateMetrics) *oapi.BitrateMetrics {
	video := float32(br.Video)
	audio := float32(br.Audio)
	total := float32(br.Total)
	return &oapi.BitrateMetrics{
		Video: &video,
		Audio: &audio,
		Total: &total,
	}
}

func convertPacketMetrics(pm benchmarks.PacketMetrics) *oapi.PacketMetrics {
	videoReceived := int(pm.VideoReceived)
	videoLost := int(pm.VideoLost)
	audioReceived := int(pm.AudioReceived)
	audioLost := int(pm.AudioLost)
	lossPercent := float32(pm.LossPercent)
	return &oapi.PacketMetrics{
		VideoReceived: &videoReceived,
		VideoLost:     &videoLost,
		AudioReceived: &audioReceived,
		AudioLost:     &audioLost,
		LossPercent:   &lossPercent,
	}
}

func convertFrameMetrics(fm benchmarks.FrameMetrics) *oapi.FrameMetrics {
	received := int(fm.Received)
	dropped := int(fm.Dropped)
	decoded := int(fm.Decoded)
	corrupted := int(fm.Corrupted)
	keyFramesDecoded := int(fm.KeyFramesDecoded)
	return &oapi.FrameMetrics{
		Received:         &received,
		Dropped:          &dropped,
		Decoded:          &decoded,
		Corrupted:        &corrupted,
		KeyFramesDecoded: &keyFramesDecoded,
	}
}

func convertJitterMetrics(jm benchmarks.JitterMetrics) *oapi.JitterMetrics {
	video := float32(jm.Video)
	audio := float32(jm.Audio)
	return &oapi.JitterMetrics{
		Video: &video,
		Audio: &audio,
	}
}

func convertNetworkMetrics(nm benchmarks.NetworkMetrics) *oapi.NetworkMetrics {
	rttMs := float32(nm.RTTMS)
	availableBitrate := float32(nm.AvailableOutgoingBitrateKbps)
	bytesReceived := int(nm.BytesReceived)
	bytesSent := int(nm.BytesSent)
	return &oapi.NetworkMetrics{
		RttMs:                        &rttMs,
		AvailableOutgoingBitrateKbps: &availableBitrate,
		BytesReceived:                &bytesReceived,
		BytesSent:                    &bytesSent,
	}
}

func convertCodecMetrics(cm benchmarks.CodecMetrics) *oapi.CodecMetrics {
	return &oapi.CodecMetrics{
		Video: &cm.Video,
		Audio: &cm.Audio,
	}
}

func convertResolutionMetrics(rm benchmarks.ResolutionMetrics) *oapi.ResolutionMetrics {
	width := rm.Width
	height := rm.Height
	return &oapi.ResolutionMetrics{
		Width:  &width,
		Height: &height,
	}
}

func convertMemoryMetrics(mem benchmarks.MemoryMetrics) *oapi.MemoryMetrics {
	baseline := float32(mem.Baseline)
	result := &oapi.MemoryMetrics{
		Baseline: &baseline,
	}
	if mem.PerConnection > 0 {
		perConn := float32(mem.PerConnection)
		result.PerConnection = &perConn
	}
	if mem.PerViewer > 0 {
		perViewer := float32(mem.PerViewer)
		result.PerViewer = &perViewer
	}
	return result
}

func optionalInt(val int64) *int {
	if val == 0 {
		return nil
	}
	casted := int(val)
	return &casted
}

func optionalString(val string) *string {
	if val == "" {
		return nil
	}
	return &val
}

func convertStartupTimingResults(timing *benchmarks.StartupTimingResults) *oapi.StartupTimingResults {
	totalMs := float32(timing.TotalStartupTimeMS)
	phases := make([]oapi.PhaseResult, len(timing.Phases))

	for i, phase := range timing.Phases {
		durationMs := float32(phase.DurationMS)
		percentage := float32(phase.Percentage)
		phases[i] = oapi.PhaseResult{
			Name:       &phase.Name,
			DurationMs: &durationMs,
			Percentage: &percentage,
		}
	}

	fastestMs := float32(timing.PhaseSummary.FastestMS)
	slowestMs := float32(timing.PhaseSummary.SlowestMS)

	result := &oapi.StartupTimingResults{
		TotalStartupTimeMs: &totalMs,
		Phases:             &phases,
		PhaseSummary: &oapi.PhaseSummary{
			FastestPhase: &timing.PhaseSummary.FastestPhase,
			SlowestPhase: &timing.PhaseSummary.SlowestPhase,
			FastestMs:    &fastestMs,
			SlowestMs:    &slowestMs,
		},
	}

	return result
}

func (s *ApiService) runCDPBenchmark(ctx context.Context) (*benchmarks.CDPProxyResults, error) {
	log := logger.FromContext(ctx)
	log.Info("running CDP benchmark")

	// CDP proxy is exposed on port 9222
	cdpProxyURL := "http://localhost:9222"
	concurrency := 1 // Sequential, consistent benchmark

	benchmark := benchmarks.NewCDPRuntimeBenchmark(log, cdpProxyURL, concurrency)
	return benchmark.Run(ctx, 0) // Duration parameter ignored, uses internal 5s
}

func (s *ApiService) runWebRTCBenchmark(ctx context.Context) (*benchmarks.WebRTCLiveViewResults, error) {
	log := logger.FromContext(ctx)
	log.Info("running WebRTC benchmark")

	// Neko is typically on localhost:8080
	nekoBaseURL := "http://127.0.0.1:8080"

	benchmark := benchmarks.NewWebRTCBenchmark(log, nekoBaseURL)
	return benchmark.Run(ctx, 0) // Duration parameter ignored, uses internal 10s
}

func (s *ApiService) runRecordingBenchmark(ctx context.Context) (*benchmarks.RecordingResults, error) {
	log := logger.FromContext(ctx)
	log.Info("running Recording benchmark")

	profiler := benchmarks.NewRecordingProfiler(log, s.recordManager, s.factory)
	return profiler.Run(ctx, 0) // Duration parameter ignored, uses internal 10s
}

func parseComponents(componentsParam *string) []benchmarks.BenchmarkComponent {
	if componentsParam == nil {
		return []benchmarks.BenchmarkComponent{benchmarks.ComponentAll}
	}

	componentsStr := *componentsParam
	if componentsStr == "" || componentsStr == "all" {
		return []benchmarks.BenchmarkComponent{
			benchmarks.ComponentCDP,
			benchmarks.ComponentWebRTC,
			benchmarks.ComponentRecording,
		}
	}

	// Parse comma-separated list
	parts := strings.Split(componentsStr, ",")
	components := make([]benchmarks.BenchmarkComponent, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "cdp":
			components = append(components, benchmarks.ComponentCDP)
		case "webrtc":
			components = append(components, benchmarks.ComponentWebRTC)
		case "recording":
			components = append(components, benchmarks.ComponentRecording)
		case "all":
			return []benchmarks.BenchmarkComponent{
				benchmarks.ComponentCDP,
				benchmarks.ComponentWebRTC,
				benchmarks.ComponentRecording,
			}
		}
	}

	if len(components) == 0 {
		// Default to all if none specified
		return []benchmarks.BenchmarkComponent{
			benchmarks.ComponentCDP,
			benchmarks.ComponentWebRTC,
			benchmarks.ComponentRecording,
		}
	}

	return components
}

func parseDuration(durationParam *int) time.Duration {
	if durationParam == nil {
		return 10 * time.Second
	}

	duration := *durationParam
	if duration < 1 {
		duration = 1
	} else if duration > 60 {
		duration = 60
	}

	return time.Duration(duration) * time.Second
}

func getSystemInfo() benchmarks.SystemInfo {
	memTotalMB := int64(0)
	if total, err := benchmarks.GetSystemMemoryTotalMB(); err == nil && total > 0 {
		memTotalMB = int64(total)
	} else {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		memTotalMB = int64(memStats.Sys / 1024 / 1024)
	}

	return benchmarks.SystemInfo{
		CPUCount:      runtime.NumCPU(),
		MemoryTotalMB: memTotalMB,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
	}
}
