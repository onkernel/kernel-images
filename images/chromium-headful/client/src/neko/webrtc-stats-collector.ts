import { EVENT } from './events'
import { BenchmarkWebRTCStatsPayload } from './messages'

/**
 * WebRTCStatsCollector collects comprehensive WebRTC statistics from the browser's RTCPeerConnection
 * similar to chrome://webrtc-internals and sends them to the server via WebSocket for benchmarking.
 */
export class WebRTCStatsCollector {
  private peerConnection?: RTCPeerConnection
  private intervalId?: number
  private sendStats: (stats: BenchmarkWebRTCStatsPayload) => void
  private collectionInterval: number = 2000 // 2 seconds
  private enabled: boolean = false

  // Track stats for rate calculations
  private lastStats?: RTCStatsReport
  private lastStatsTime?: number

  // Accumulated values for percentile calculations
  private frameRates: number[] = []
  private videoBitrates: number[] = []
  private audioBitrates: number[] = []
  private frameTimes: number[] = []

  constructor(sendStatsCallback: (stats: BenchmarkWebRTCStatsPayload) => void) {
    this.sendStats = sendStatsCallback
  }

  /**
   * Start collecting stats from the given peer connection
   */
  public start(peerConnection: RTCPeerConnection): void {
    if (this.enabled) {
      return
    }

    this.peerConnection = peerConnection
    this.enabled = true
    this.frameRates = []
    this.videoBitrates = []
    this.audioBitrates = []
    this.frameTimes = []

    // Collect stats periodically
    this.intervalId = window.setInterval(() => {
      this.collectAndSendStats()
    }, this.collectionInterval)

    // Send initial stats immediately
    this.collectAndSendStats()
  }

  /**
   * Stop collecting stats
   */
  public stop(): void {
    if (!this.enabled) {
      return
    }

    if (this.intervalId) {
      window.clearInterval(this.intervalId)
      this.intervalId = undefined
    }

    this.enabled = false
    this.peerConnection = undefined
    this.lastStats = undefined
    this.lastStatsTime = undefined
    this.frameRates = []
    this.videoBitrates = []
    this.audioBitrates = []
    this.frameTimes = []
  }

  /**
   * Collect current stats and send them to server
   */
  private async collectAndSendStats(): Promise<void> {
    if (!this.peerConnection || !this.enabled) {
      return
    }

    try {
      const stats = await this.peerConnection.getStats()
      const now = performance.now()

      // Process stats
      const processedStats = this.processStats(stats, now)

      if (processedStats) {
        this.sendStats(processedStats)
      }

      this.lastStats = stats
      this.lastStatsTime = now
    } catch (error) {
      console.error('[WebRTCStatsCollector] Error collecting stats:', error)
    }
  }

  /**
   * Process raw WebRTC stats into our comprehensive benchmark format
   */
  private processStats(stats: RTCStatsReport, now: number): BenchmarkWebRTCStatsPayload | null {
    // Find all relevant stats
    let inboundVideoStats: any = null
    let inboundAudioStats: any = null
    let candidatePairStats: any = null
    let videoTrackStats: any = null
    let audioTrackStats: any = null
    let videoCodecStats: any = null
    let audioCodecStats: any = null

    stats.forEach((stat) => {
      switch (stat.type) {
        case 'inbound-rtp':
          if (stat.kind === 'video') {
            inboundVideoStats = stat
          } else if (stat.kind === 'audio') {
            inboundAudioStats = stat
          }
          break
        case 'candidate-pair':
          if (stat.state === 'succeeded') {
            candidatePairStats = stat
          }
          break
        case 'track':
          if (stat.kind === 'video') {
            videoTrackStats = stat
          } else if (stat.kind === 'audio') {
            audioTrackStats = stat
          }
          break
        case 'codec':
          if (stat.mimeType?.startsWith('video/')) {
            videoCodecStats = stat
          } else if (stat.mimeType?.startsWith('audio/')) {
            audioCodecStats = stat
          }
          break
      }
    })

    if (!inboundVideoStats) {
      return null // Can't generate meaningful stats without video
    }

    // Get last stats for rate calculations
    let lastVideoStats: any = null
    let lastAudioStats: any = null
    let lastCandidatePairStats: any = null

    if (this.lastStats) {
      this.lastStats.forEach((stat) => {
        if (stat.type === 'inbound-rtp' && stat.kind === 'video') {
          lastVideoStats = stat
        } else if (stat.type === 'inbound-rtp' && stat.kind === 'audio') {
          lastAudioStats = stat
        } else if (stat.type === 'candidate-pair' && stat.state === 'succeeded') {
          lastCandidatePairStats = stat
        }
      })
    }

    // Calculate rates
    const deltaTime = this.lastStatsTime ? (now - this.lastStatsTime) / 1000 : 0 // seconds

    // Frame rate
    let currentFPS = 0
    if (lastVideoStats && deltaTime > 0) {
      const deltaFrames = (inboundVideoStats.framesReceived || 0) - (lastVideoStats.framesReceived || 0)
      currentFPS = deltaFrames / deltaTime
    }

    // Video bitrate
    let currentVideoBitrate = 0
    if (lastVideoStats && deltaTime > 0) {
      const deltaBytes = (inboundVideoStats.bytesReceived || 0) - (lastVideoStats.bytesReceived || 0)
      currentVideoBitrate = (deltaBytes * 8) / (deltaTime * 1000) // kbps
    }

    // Audio bitrate
    let currentAudioBitrate = 0
    if (lastAudioStats && deltaTime > 0) {
      const deltaBytes = (inboundAudioStats?.bytesReceived || 0) - (lastAudioStats.bytesReceived || 0)
      currentAudioBitrate = (deltaBytes * 8) / (deltaTime * 1000) // kbps
    }

    // Track values for percentiles
    if (currentFPS > 0) {
      this.frameRates.push(currentFPS)
      const frameTime = 1000 / currentFPS // ms per frame
      this.frameTimes.push(frameTime)

      // Keep only last 100 samples
      if (this.frameRates.length > 100) {
        this.frameRates.shift()
        this.frameTimes.shift()
      }
    }

    if (currentVideoBitrate > 0) {
      this.videoBitrates.push(currentVideoBitrate)
      if (this.videoBitrates.length > 100) {
        this.videoBitrates.shift()
      }
    }

    if (currentAudioBitrate > 0) {
      this.audioBitrates.push(currentAudioBitrate)
      if (this.audioBitrates.length > 100) {
        this.audioBitrates.shift()
      }
    }

    // Calculate metrics
    const frameRateMetrics = this.calculateFrameRateMetrics(currentFPS)
    const frameLatencyMetrics = this.calculateLatencyPercentiles(this.frameTimes)

    // Bitrate metrics
    const avgVideoBitrate = this.videoBitrates.length > 0
      ? this.videoBitrates.reduce((a, b) => a + b, 0) / this.videoBitrates.length
      : currentVideoBitrate

    const avgAudioBitrate = this.audioBitrates.length > 0
      ? this.audioBitrates.reduce((a, b) => a + b, 0) / this.audioBitrates.length
      : currentAudioBitrate

    // Packet metrics
    const videoPacketsReceived = inboundVideoStats.packetsReceived || 0
    const videoPacketsLost = inboundVideoStats.packetsLost || 0
    const audioPacketsReceived = inboundAudioStats?.packetsReceived || 0
    const audioPacketsLost = inboundAudioStats?.packetsLost || 0

    const totalPacketsReceived = videoPacketsReceived + audioPacketsReceived
    const totalPacketsLost = videoPacketsLost + audioPacketsLost
    const packetLossPercent = totalPacketsReceived > 0
      ? (totalPacketsLost / (totalPacketsReceived + totalPacketsLost)) * 100
      : 0

    // Frame metrics
    const framesReceived = inboundVideoStats.framesReceived || 0
    const framesDropped = inboundVideoStats.framesDropped || 0
    const framesDecoded = inboundVideoStats.framesDecoded || 0
    const framesCorrupted = videoTrackStats?.framesCorrupted || 0
    const keyFramesDecoded = inboundVideoStats.keyFramesDecoded || 0

    // Network metrics
    const rtt = candidatePairStats?.currentRoundTripTime
      ? candidatePairStats.currentRoundTripTime * 1000 // Convert to ms
      : 0

    const availableOutgoingBitrate = candidatePairStats?.availableOutgoingBitrate
      ? candidatePairStats.availableOutgoingBitrate / 1000 // Convert to kbps
      : 0

    const bytesReceived = candidatePairStats?.bytesReceived || 0
    const bytesSent = candidatePairStats?.bytesSent || 0

    // Jitter
    const videoJitter = inboundVideoStats.jitter ? inboundVideoStats.jitter * 1000 : 0 // ms
    const audioJitter = inboundAudioStats?.jitter ? inboundAudioStats.jitter * 1000 : 0 // ms

    // Codecs
    const videoCodec = videoCodecStats?.mimeType || 'unknown'
    const audioCodec = audioCodecStats?.mimeType || 'unknown'

    // Resolution
    const width = inboundVideoStats.frameWidth || videoTrackStats?.frameWidth || 0
    const height = inboundVideoStats.frameHeight || videoTrackStats?.frameHeight || 0

    // Connection states
    const connectionState = this.peerConnection?.connectionState || 'unknown'
    const iceConnectionState = this.peerConnection?.iceConnectionState || 'unknown'

    return {
      timestamp: new Date().toISOString(),
      connection_state: connectionState,
      ice_connection_state: iceConnectionState,
      frame_rate_fps: frameRateMetrics,
      frame_latency_ms: frameLatencyMetrics,
      bitrate_kbps: {
        video: avgVideoBitrate,
        audio: avgAudioBitrate,
        total: avgVideoBitrate + avgAudioBitrate,
      },
      packets: {
        video_received: videoPacketsReceived,
        video_lost: videoPacketsLost,
        audio_received: audioPacketsReceived,
        audio_lost: audioPacketsLost,
        loss_percent: packetLossPercent,
      },
      frames: {
        received: framesReceived,
        dropped: framesDropped,
        decoded: framesDecoded,
        corrupted: framesCorrupted,
        key_frames_decoded: keyFramesDecoded,
      },
      jitter_ms: {
        video: videoJitter,
        audio: audioJitter,
      },
      network: {
        rtt_ms: rtt,
        available_outgoing_bitrate_kbps: availableOutgoingBitrate,
        bytes_received: bytesReceived,
        bytes_sent: bytesSent,
      },
      codecs: {
        video: videoCodec,
        audio: audioCodec,
      },
      resolution: {
        width,
        height,
      },
      concurrent_viewers: 1, // Client always sees itself as 1 viewer
    }
  }

  /**
   * Calculate frame rate metrics
   */
  private calculateFrameRateMetrics(currentFPS: number) {
    const target = 30 // Assuming 30fps target
    const achieved = this.frameRates.length > 0
      ? this.frameRates.reduce((a, b) => a + b, 0) / this.frameRates.length
      : currentFPS

    const min = this.frameRates.length > 0 ? Math.min(...this.frameRates) : currentFPS
    const max = this.frameRates.length > 0 ? Math.max(...this.frameRates) : currentFPS

    return {
      target,
      achieved: achieved || 0,
      min: min || 0,
      max: max || 0,
    }
  }

  /**
   * Calculate percentiles from an array of values
   */
  private calculateLatencyPercentiles(values: number[]) {
    if (values.length === 0) {
      return { p50: 33.3, p95: 50, p99: 67 } // Default for 30fps
    }

    const sorted = [...values].sort((a, b) => a - b)
    const p50Idx = Math.floor(sorted.length * 0.50)
    const p95Idx = Math.floor(sorted.length * 0.95)
    const p99Idx = Math.floor(sorted.length * 0.99)

    return {
      p50: sorted[Math.min(p50Idx, sorted.length - 1)] || 0,
      p95: sorted[Math.min(p95Idx, sorted.length - 1)] || 0,
      p99: sorted[Math.min(p99Idx, sorted.length - 1)] || 0,
    }
  }
}
