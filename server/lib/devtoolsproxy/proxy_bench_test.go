package devtoolsproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/onkernel/kernel-images/server/lib/scaletozero"
)

// BenchmarkWebSocketProxyThroughput measures message throughput through the proxy
func BenchmarkWebSocketProxyThroughput(b *testing.B) {
	echoSrv := startEchoServer(b)
	defer echoSrv.Close()

	mgr, proxySrv := setupProxy(b, echoSrv.URL)
	defer proxySrv.Close()
	_ = mgr

	ctx := context.Background()
	conn := connectToProxy(b, ctx, proxySrv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Simple message for throughput testing
	msg := []byte(`{"id":1,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}

	throughput := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput, "msgs/sec")
}

// BenchmarkWebSocketProxyLatency measures round-trip latency
func BenchmarkWebSocketProxyLatency(b *testing.B) {
	echoSrv := startEchoServer(b)
	defer echoSrv.Close()

	mgr, proxySrv := setupProxy(b, echoSrv.URL)
	defer proxySrv.Close()
	_ = mgr

	ctx := context.Background()
	conn := connectToProxy(b, ctx, proxySrv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	msg := []byte(`{"id":1,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)

	b.ResetTimer()

	var totalLatency time.Duration
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil {
			b.Fatalf("read failed: %v", err)
		}
		totalLatency += time.Since(start)
	}

	avgLatencyMs := float64(totalLatency.Microseconds()) / float64(b.N) / 1000.0
	b.ReportMetric(avgLatencyMs, "ms/op")
}

// BenchmarkWebSocketProxyMessageSizes tests performance with different message sizes
func BenchmarkWebSocketProxyMessageSizes(b *testing.B) {
	sizes := []int{
		100,    // Small CDP command
		1024,   // 1KB - typical CDP response
		10240,  // 10KB - larger DOM query result
		102400, // 100KB - screenshot data
		524288, // 512KB - large data transfer
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			echoSrv := startEchoServer(b)
			defer echoSrv.Close()

			mgr, proxySrv := setupProxy(b, echoSrv.URL)
			defer proxySrv.Close()
			_ = mgr

			ctx := context.Background()
			conn := connectToProxy(b, ctx, proxySrv.URL)
			defer conn.Close(websocket.StatusNormalClosure, "")

			// Create message of specified size
			msg := make([]byte, size)
			for i := range msg {
				msg[i] = 'x'
			}

			b.ResetTimer()
			b.SetBytes(int64(size))

			for i := 0; i < b.N; i++ {
				if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
					b.Fatalf("write failed: %v", err)
				}
				if _, _, err := conn.Read(ctx); err != nil {
					b.Fatalf("read failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkWebSocketProxyConcurrentConnections tests concurrent connection handling
func BenchmarkWebSocketProxyConcurrentConnections(b *testing.B) {
	connections := []int{1, 5, 10, 20, 50}

	for _, numConns := range connections {
		b.Run(fmt.Sprintf("conns_%d", numConns), func(b *testing.B) {
			echoSrv := startEchoServer(b)
			defer echoSrv.Close()

			mgr, proxySrv := setupProxy(b, echoSrv.URL)
			defer proxySrv.Close()
			_ = mgr

			ctx := context.Background()
			msg := []byte(`{"id":1,"method":"Runtime.evaluate","params":{"expression":"1+1"}}`)

			b.ResetTimer()

			// Create connection pool
			var wg sync.WaitGroup
			var totalOps atomic.Int64

			for c := 0; c < numConns; c++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					conn := connectToProxy(b, ctx, proxySrv.URL)
					defer conn.Close(websocket.StatusNormalClosure, "")

					opsPerConn := b.N / numConns
					for i := 0; i < opsPerConn; i++ {
						if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
							b.Errorf("write failed: %v", err)
							return
						}
						if _, _, err := conn.Read(ctx); err != nil {
							b.Errorf("read failed: %v", err)
							return
						}
						totalOps.Add(1)
					}
				}()
			}

			wg.Wait()

			throughput := float64(totalOps.Load()) / b.Elapsed().Seconds()
			b.ReportMetric(throughput, "msgs/sec")
		})
	}
}

// Helper functions

func startEchoServer(b *testing.B) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			b.Fatalf("accept failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		for {
			mt, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			if err := c.Write(ctx, mt, msg); err != nil {
				return
			}
		}
	}))
}

func setupProxy(b *testing.B, echoURL string) (*UpstreamManager, *httptest.Server) {
	u, _ := url.Parse(echoURL)
	u.Scheme = "ws"
	u.Path = "/devtools"

	logger := silentLogger()
	mgr := NewUpstreamManager("/dev/null", logger)
	mgr.setCurrent(u.String())

	proxy := WebSocketProxyHandler(mgr, logger, false, scaletozero.NewNoopController())
	proxySrv := httptest.NewServer(proxy)

	return mgr, proxySrv
}

func connectToProxy(b *testing.B, ctx context.Context, proxyURL string) *websocket.Conn {
	pu, _ := url.Parse(proxyURL)
	pu.Scheme = "ws"

	conn, _, err := websocket.Dial(ctx, pu.String(), nil)
	if err != nil {
		b.Fatalf("dial proxy failed: %v", err)
	}
	return conn
}
