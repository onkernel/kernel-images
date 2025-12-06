package stream

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/notedit/rtmp/format/rtmp"
	"github.com/notedit/rtmp/pubsub"
)

// RTMPServer implements an internal RTMP/RTMPS server backed by pubsub.
type RTMPServer struct {
	mu          sync.Mutex
	rtmpAddr    string
	rtmpsAddr   string
	tlsConfig   *tls.Config
	streams     map[string]*pubsub.PubSub
	listener    net.Listener
	tlsListener net.Listener
	server      *rtmp.Server
	running     bool
	logger      *slog.Logger
	wg          sync.WaitGroup
}

func NewRTMPServer(rtmpAddr, rtmpsAddr string, tlsConfig *tls.Config, logger *slog.Logger) *RTMPServer {
	return &RTMPServer{
		rtmpAddr:  rtmpAddr,
		rtmpsAddr: rtmpsAddr,
		tlsConfig: tlsConfig,
		streams:   make(map[string]*pubsub.PubSub),
		logger:    logger,
	}
}

func (s *RTMPServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if s.streams == nil {
		s.streams = make(map[string]*pubsub.PubSub)
	}

	rtmpListener, err := net.Listen("tcp", s.rtmpAddr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to start rtmp listener: %w", err)
	}

	srv := rtmp.NewServer()
	srv.HandleConn = func(c *rtmp.Conn, nc net.Conn) {
		s.handleConn(ctx, c, nc)
	}
	srv.LogEvent = func(c *rtmp.Conn, nc net.Conn, e int) {
		if s.logger == nil {
			return
		}
		s.logger.Debug("rtmp event", slog.String("event", rtmp.EventString[e]), slog.String("remote_addr", nc.RemoteAddr().String()))
	}

	s.listener = rtmpListener
	s.server = srv
	s.running = true
	s.mu.Unlock()

	if s.logger != nil {
		s.logger.Info("rtmp listener started", slog.String("addr", rtmpListener.Addr().String()))
	}
	s.wg.Add(1)
	go s.acceptLoop(ctx, rtmpListener, false)

	if s.tlsConfig != nil && s.rtmpsAddr != "" {
		tlsListener, err := tls.Listen("tcp", s.rtmpsAddr, s.tlsConfig)
		if err != nil {
			_ = rtmpListener.Close()
			return fmt.Errorf("failed to start rtmps listener: %w", err)
		}
		s.mu.Lock()
		s.tlsListener = tlsListener
		s.mu.Unlock()

		if s.logger != nil {
			s.logger.Info("rtmps listener started", slog.String("addr", tlsListener.Addr().String()))
		}
		s.wg.Add(1)
		go s.acceptLoop(ctx, tlsListener, true)
	}

	return nil
}

func (s *RTMPServer) acceptLoop(ctx context.Context, ln net.Listener, secure bool) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if s.logger != nil {
				s.logger.Warn("rtmp accept error", slog.String("err", err.Error()), slog.Bool("secure", secure))
			}
			continue
		}
		go s.server.HandleNetConn(conn)
	}
}

func (s *RTMPServer) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	rtmpListener := s.listener
	tlsListener := s.tlsListener
	s.mu.Unlock()

	if rtmpListener != nil {
		_ = rtmpListener.Close()
	}
	if tlsListener != nil {
		_ = tlsListener.Close()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.wg.Wait()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *RTMPServer) EnsureStream(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.streams[path]; !ok {
		s.streams[path] = &pubsub.PubSub{}
	}
}

func (s *RTMPServer) IngestURL(streamPath string) string {
	port := s.listenPort(s.rtmpAddr, "1935")
	hostPort := net.JoinHostPort("127.0.0.1", port)
	return fmt.Sprintf("rtmp://%s/%s", hostPort, strings.TrimPrefix(streamPath, "/"))
}

func (s *RTMPServer) PlaybackURLs(host string, streamPath string) (rtmpURL *string, rtmpsURL *string) {
	cleanHost := stripPort(host)
	if cleanHost == "" {
		cleanHost = "127.0.0.1"
	}
	streamPath = strings.TrimPrefix(streamPath, "/")

	if s.rtmpAddr != "" {
		port := s.listenPort(s.rtmpAddr, "1935")
		hostPort := net.JoinHostPort(cleanHost, port)
		url := fmt.Sprintf("rtmp://%s/%s", hostPort, streamPath)
		rtmpURL = &url
	}

	s.mu.Lock()
	tlsListener := s.tlsListener
	s.mu.Unlock()
	if tlsListener != nil && s.rtmpsAddr != "" {
		port := s.listenPort(s.rtmpsAddr, "1936")
		hostPort := net.JoinHostPort(cleanHost, port)
		url := fmt.Sprintf("rtmps://%s/%s", hostPort, streamPath)
		rtmpsURL = &url
	}

	return rtmpURL, rtmpsURL
}

func (s *RTMPServer) handleConn(ctx context.Context, c *rtmp.Conn, nc net.Conn) {
	if err := c.Prepare(rtmp.StageGotPublishOrPlayCommand, 0); err != nil {
		if s.logger != nil {
			s.logger.Error("rtmp handshake failed", slog.String("err", err.Error()))
		}
		_ = nc.Close()
		return
	}

	path := strings.TrimPrefix(c.URL.Path, "/")
	if path == "" {
		path = "live/default"
	}

	ps := s.ensurePubSub(path)
	if c.Publishing {
		if s.logger != nil {
			s.logger.Info("rtmp publisher connected", slog.String("path", path))
		}
		go func() {
			ps.SetPub(c)
			_ = nc.Close()
			if s.logger != nil {
				s.logger.Info("rtmp publisher disconnected", slog.String("path", path))
			}
		}()
	} else {
		if s.logger != nil {
			s.logger.Info("rtmp subscriber connected", slog.String("path", path))
		}
		done := make(chan bool, 1)
		go func() {
			<-c.CloseNotify()
			close(done)
			if s.logger != nil {
				s.logger.Info("rtmp subscriber disconnected", slog.String("path", path))
			}
		}()
		go ps.AddSub(done, c)
	}
}

func (s *RTMPServer) ensurePubSub(path string) *pubsub.PubSub {
	s.mu.Lock()
	defer s.mu.Unlock()

	ps, ok := s.streams[path]
	if !ok {
		ps = &pubsub.PubSub{}
		s.streams[path] = ps
	}
	return ps
}

func (s *RTMPServer) listenPort(addr string, defaultPort string) string {
	_, port, err := net.SplitHostPort(addr)
	if err == nil && port != "" {
		return port
	}
	if strings.HasPrefix(addr, ":") {
		return strings.TrimPrefix(addr, ":")
	}
	return defaultPort
}

func stripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

// SelfSignedTLSConfig generates a minimal self-signed TLS config for RTMPS.
func SelfSignedTLSConfig() (*tls.Config, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("tls key pair: %w", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
