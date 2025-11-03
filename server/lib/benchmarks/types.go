package benchmarks

import "time"

// BenchmarkResults represents the complete benchmark output
type BenchmarkResults struct {
	Timestamp      time.Time             `json:"timestamp"`
	ElapsedSeconds float64               `json:"elapsed_seconds"` // Actual elapsed time of all benchmarks
	System         SystemInfo            `json:"system"`
	Results        ComponentResults      `json:"results"`
	Errors         []string              `json:"errors"`
	StartupTiming  *StartupTimingResults `json:"startup_timing,omitempty"`
}

// SystemInfo contains system information
type SystemInfo struct {
	CPUCount        int    `json:"cpu_count"`
	MemoryTotalMB   int64  `json:"memory_total_mb"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
}

// ComponentResults contains results for each benchmarked component
type ComponentResults struct {
	CDP            *CDPProxyResults       `json:"cdp,omitempty"`
	WebRTCLiveView *WebRTCLiveViewResults `json:"webrtc_live_view,omitempty"`
	Recording      *RecordingResults      `json:"recording,omitempty"`
}

// CDPProxyResults contains CDP proxy benchmark results
type CDPProxyResults struct {
	ThroughputMsgsPerSec   float64                `json:"throughput_msgs_per_sec"`
	LatencyMS              LatencyMetrics         `json:"latency_ms"`
	ConcurrentConnections  int                    `json:"concurrent_connections"`
	MemoryMB               MemoryMetrics          `json:"memory_mb"`
	MessageSizeBytes       MessageSizeMetrics     `json:"message_size_bytes"`
	Scenarios              []CDPScenarioResult    `json:"scenarios,omitempty"`
	ProxiedEndpoint        *CDPEndpointResults    `json:"proxied_endpoint,omitempty"`
	DirectEndpoint         *CDPEndpointResults    `json:"direct_endpoint,omitempty"`
	ProxyOverheadPercent   float64                `json:"proxy_overhead_percent,omitempty"`
}

// CDPEndpointResults contains results for a specific CDP endpoint (proxied or direct)
type CDPEndpointResults struct {
	EndpointURL          string                 `json:"endpoint_url"`
	ThroughputMsgsPerSec float64                `json:"throughput_msgs_per_sec"`
	LatencyMS            LatencyMetrics         `json:"latency_ms"`
	Scenarios            []CDPScenarioResult    `json:"scenarios,omitempty"`
}

// CDPScenarioResult contains results for a specific CDP scenario
type CDPScenarioResult struct {
	Name                 string           `json:"name"`
	Description          string           `json:"description"`
	Category             string           `json:"category"`
	OperationCount       int64            `json:"operation_count"`
	ThroughputOpsPerSec  float64          `json:"throughput_ops_per_sec"`
	LatencyMS            LatencyMetrics   `json:"latency_ms"`
	SuccessRate          float64          `json:"success_rate"`
}

// WebRTCLiveViewResults contains comprehensive WebRTC live view benchmark results
type WebRTCLiveViewResults struct {
	ConnectionState    string                 `json:"connection_state"`
	IceConnectionState string                 `json:"ice_connection_state"`
	FrameRateFPS       FrameRateMetrics       `json:"frame_rate_fps"`
	FrameLatencyMS     LatencyMetrics         `json:"frame_latency_ms"`
	BitrateKbps        BitrateMetrics         `json:"bitrate_kbps"`
	Packets            PacketMetrics          `json:"packets"`
	Frames             FrameMetrics           `json:"frames"`
	JitterMS           JitterMetrics          `json:"jitter_ms"`
	Network            NetworkMetrics         `json:"network"`
	Codecs             CodecMetrics           `json:"codecs"`
	Resolution         ResolutionMetrics      `json:"resolution"`
	ConcurrentViewers  int                    `json:"concurrent_viewers"`
	CPUUsagePercent    float64                `json:"cpu_usage_percent"`
	MemoryMB           MemoryMetrics          `json:"memory_mb"`
}

// RecordingResults contains recording benchmark results
type RecordingResults struct {
	CPUOverheadPercent    float64                       `json:"cpu_overhead_percent"`
	MemoryOverheadMB      float64                       `json:"memory_overhead_mb"`
	FramesCaptured        int64                         `json:"frames_captured"`
	FramesDropped         int64                         `json:"frames_dropped"`
	AvgEncodingLagMS      float64                       `json:"avg_encoding_lag_ms"`
	DiskWriteMBPS         float64                       `json:"disk_write_mbps"`
	ConcurrentRecordings  int                           `json:"concurrent_recordings"`
	FrameRateImpact       *RecordingFrameRateImpact     `json:"frame_rate_impact,omitempty"`
}

// RecordingFrameRateImpact shows how recording affects live view frame rate
type RecordingFrameRateImpact struct {
	BeforeRecordingFPS float64 `json:"before_recording_fps"`
	DuringRecordingFPS float64 `json:"during_recording_fps"`
	ImpactPercent      float64 `json:"impact_percent"`
}

// LatencyMetrics contains latency percentiles
type LatencyMetrics struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// FrameRateMetrics contains frame rate statistics
type FrameRateMetrics struct {
	Target   float64 `json:"target"`
	Achieved float64 `json:"achieved"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
}

// BitrateMetrics contains bitrate statistics
type BitrateMetrics struct {
	Video float64 `json:"video"`
	Audio float64 `json:"audio"`
	Total float64 `json:"total"`
}

// PacketMetrics contains packet statistics
type PacketMetrics struct {
	VideoReceived int64   `json:"video_received"`
	VideoLost     int64   `json:"video_lost"`
	AudioReceived int64   `json:"audio_received"`
	AudioLost     int64   `json:"audio_lost"`
	LossPercent   float64 `json:"loss_percent"`
}

// FrameMetrics contains frame statistics
type FrameMetrics struct {
	Received         int64 `json:"received"`
	Dropped          int64 `json:"dropped"`
	Decoded          int64 `json:"decoded"`
	Corrupted        int64 `json:"corrupted"`
	KeyFramesDecoded int64 `json:"key_frames_decoded"`
}

// JitterMetrics contains jitter statistics
type JitterMetrics struct {
	Video float64 `json:"video"`
	Audio float64 `json:"audio"`
}

// NetworkMetrics contains network statistics
type NetworkMetrics struct {
	RTTMS                        float64 `json:"rtt_ms"`
	AvailableOutgoingBitrateKbps float64 `json:"available_outgoing_bitrate_kbps"`
	BytesReceived                int64   `json:"bytes_received"`
	BytesSent                    int64   `json:"bytes_sent"`
}

// CodecMetrics contains codec information
type CodecMetrics struct {
	Video string `json:"video"`
	Audio string `json:"audio"`
}

// ResolutionMetrics contains resolution information
type ResolutionMetrics struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// MemoryMetrics contains memory usage statistics
type MemoryMetrics struct {
	Baseline      float64 `json:"baseline"`
	PerConnection float64 `json:"per_connection,omitempty"`
	PerViewer     float64 `json:"per_viewer,omitempty"`
}

// MessageSizeMetrics contains message size statistics
type MessageSizeMetrics struct {
	Min int `json:"min"`
	Max int `json:"max"`
	Avg int `json:"avg"`
}

// BenchmarkComponent represents which component to benchmark
type BenchmarkComponent string

const (
	ComponentCDP       BenchmarkComponent = "cdp"
	ComponentWebRTC    BenchmarkComponent = "webrtc"
	ComponentRecording BenchmarkComponent = "recording"
	ComponentAll       BenchmarkComponent = "all"
)

// BenchmarkConfig contains configuration for running benchmarks
type BenchmarkConfig struct {
	Components []BenchmarkComponent
	Duration   time.Duration
}
