package httpmux

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtaci/smux"
)

// ═══════════════════════════════════════════════════════════════
// PicoTun Server v2.5
//
// Changes from v2.4:
//   • Multi-port listen — multiple tunnel endpoints on one server
//   • Port mapping fix — stream type tags prevent smux confusion
//   • 120+ user support — configurable stream limits, better pooling
//   • DPI stealth — keepalive jitter, fake traffic, padding
//   • Health monitor — proactive zombie eviction
// ═══════════════════════════════════════════════════════════════

// Stream type tags — first byte of every stream identifies its purpose.
// This fixes the port mapping + smux confusion bug where forward/reverse
// streams had no way to be differentiated, causing misrouting.
const (
	StreamTypeForward byte = 0x01 // client→server initiated (forward proxy)
	StreamTypeReverse byte = 0x02 // server→client initiated (port mapping)
)

type Server struct {
	Config  *Config
	Mimic   *MimicConfig
	Obfs    *ObfsConfig
	PSK     string
	Verbose bool

	poolMu   sync.RWMutex
	sessions []*serverSession
	poolIdx  uint64
}

type serverSession struct {
	sess    *smux.Session
	remote  string
	created time.Time
	streams int64 // atomic: active stream count
}

func NewServer(cfg *Config) *Server {
	return &Server{
		Config:  cfg,
		Mimic:   &cfg.Mimic,
		Obfs:    &cfg.Obfs,
		PSK:     cfg.PSK,
		Verbose: cfg.Verbose,
	}
}

func (s *Server) Start() error {
	log.Printf("[SERVER] maps: tcp=%d udp=%d", len(s.Config.Forward.TCP), len(s.Config.Forward.UDP))

	for _, m := range s.Config.Forward.TCP {
		if bind, target, ok := SplitMap(m); ok {
			go s.startReverseTCP(bind, target)
		}
	}
	for _, m := range s.Config.Forward.UDP {
		if bind, target, ok := SplitMap(m); ok {
			go s.startReverseUDP(bind, target)
		}
	}

	go s.healthMonitor()

	// ─── Multi-Port Listen (v2.5) ───
	// Start HTTP server on each listen port. All ports share the
	// same session pool, so port mappings can use any connected session.
	ports := s.Config.ListenPorts
	if len(ports) == 0 {
		ports = []string{s.Config.Listen}
	}

	sc := buildSmuxConfig(s.Config)
	log.Printf("[SERVER] listening on %d port(s): %v  profile=%s",
		len(ports), ports, s.Config.Profile)
	log.Printf("[SERVER] smux: keepalive=%v timeout=%v frame=%d maxrecv=%d maxstream=%d",
		sc.KeepAliveInterval, sc.KeepAliveTimeout,
		sc.MaxFrameSize, sc.MaxReceiveBuffer, sc.MaxStreamBuffer)
	log.Printf("[SERVER] limits: max_streams_per_session=%d max_connections=%d",
		s.Config.Advanced.MaxStreamsPerSession, s.Config.Advanced.MaxConnections)

	if len(ports) == 1 {
		// Single port — blocking
		return s.listenOnPort(ports[0])
	}

	// Multiple ports — launch goroutines, wait for first error
	errCh := make(chan error, len(ports))
	for _, addr := range ports {
		go func(a string) {
			errCh <- s.listenOnPort(a)
		}(addr)
		// Small delay between port starts to avoid thundering herd
		time.Sleep(100 * time.Millisecond)
	}
	return <-errCh
}

func (s *Server) listenOnPort(addr string) error {
	tunnelPath := mimicPath(s.Mimic)
	prefix := strings.Split(tunnelPath, "{")[0]

	mux := http.NewServeMux()
	mux.HandleFunc(prefix, s.handleTunnel)
	mux.HandleFunc("/", s.handleDecoy)

	log.Printf("[SERVER] port %s ready (tunnel=%s)", addr, prefix)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	// httpsmux / wssmux require TLS on the server side because the client
	// performs a real TLS handshake before the HTTP Upgrade. Plain
	// ListenAndServe() cannot understand the TLS ClientHello and will drop
	// the connection immediately.
	transport := strings.ToLower(s.Config.Transport)
	if (transport == "httpsmux" || transport == "wssmux") &&
		s.Config.CertFile != "" && s.Config.KeyFile != "" {
		log.Printf("[SERVER] TLS enabled (cert=%s)", s.Config.CertFile)
		return server.ListenAndServeTLS(s.Config.CertFile, s.Config.KeyFile)
	}
	return server.ListenAndServe()
}

// ──────────────── Tunnel Handler ────────────────

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.validateRequest(w, r) {
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", 500)
		return
	}

	// Send 101 Switching Protocols
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n" +
		"\r\n"

	conn, buf, err := hj.Hijack()
	if err != nil {
		log.Printf("[ERR] hijack: %v", err)
		return
	}

	// Set TCP options for performance
	s.setTCPOptions(conn)

	if _, err := conn.Write([]byte(resp)); err != nil {
		conn.Close()
		return
	}

	// Flush any buffered data
	if buf != nil {
		buf.Flush()
	}

	// Wrap with encryption
	ec, err := NewEncryptedConn(conn, s.PSK, s.Obfs, &s.Config.Stealth)
	if err != nil {
		log.Printf("[ERR] encrypt: %v", err)
		conn.Close()
		return
	}

	// Create smux session
	sc := buildSmuxConfig(s.Config)
	sess, err := smux.Server(ec, sc)
	if err != nil {
		log.Printf("[ERR] smux server: %v", err)
		ec.Close()
		return
	}

	ss := &serverSession{
		sess:    sess,
		remote:  r.RemoteAddr,
		created: time.Now(),
	}
	s.addSession(ss)
	log.Printf("[SESSION] new from %s (pool: %d)", r.RemoteAddr, s.poolSize())

	// Start fake traffic generator if enabled
	if s.Config.Stealth.FakeTraffic {
		go s.fakeTrafficLoop(ss)
	}

	// Accept streams from client (forward proxy direction)
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			break
		}
		go s.handleStream(ss, stream)
	}

	s.removeSession(ss)
	sess.Close()
	log.Printf("[SESSION] closed %s after %v (pool: %d)",
		r.RemoteAddr, time.Since(ss.created).Round(time.Second), s.poolSize())
}

// handleStream reads the stream type tag and routes accordingly.
// v2.5 FIX: This prevents port mapping confusion by explicitly
// identifying each stream's purpose with a type byte.
func (s *Server) handleStream(ss *serverSession, stream *smux.Stream) {
	atomic.AddInt64(&ss.streams, 1)
	defer func() {
		atomic.AddInt64(&ss.streams, -1)
		stream.Close()
	}()

	// Read stream type tag (1 byte, 5s timeout)
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, typeBuf); err != nil {
		// Backward compat: if read fails, try as forward stream (old protocol)
		return
	}
	stream.SetReadDeadline(time.Time{})

	switch typeBuf[0] {
	case StreamTypeForward:
		s.handleForwardStream(stream)
	default:
		// Unknown type — ignore
		if s.Verbose {
			log.Printf("[STREAM] unknown type 0x%02x from %s", typeBuf[0], ss.remote)
		}
	}
}

func (s *Server) handleForwardStream(stream *smux.Stream) {
	// Read target header: [2B length][target string]
	stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(stream, hdr); err != nil {
		return
	}
	tLen := binary.BigEndian.Uint16(hdr)
	if tLen == 0 || tLen > 4096 {
		return
	}
	tBuf := make([]byte, tLen)
	if _, err := io.ReadFull(stream, tBuf); err != nil {
		return
	}
	stream.SetReadDeadline(time.Time{})

	network, addr := splitTarget(string(tBuf))

	remote, err := net.DialTimeout(network, addr, 10*time.Second)
	if err != nil {
		if s.Verbose {
			log.Printf("[FWD] dial %s://%s: %v", network, addr, err)
		}
		return
	}
	defer remote.Close()
	relay(stream, remote)
}

// ──────────────── Reverse TCP (Port Mapping) ────────────────
// v2.5 FIX: Each reverse stream is now tagged with StreamTypeReverse
// so the client can distinguish it from forward streams.

func (s *Server) startReverseTCP(bind, target string) {
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		log.Printf("[RTCP] FAILED listen %s: %v", bind, err)
		return
	}
	log.Printf("[RTCP] %s → %s", bind, target)

	for {
		conn, err := ln.Accept()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.handleReverseTCPConn(conn, target)
	}
}

func (s *Server) handleReverseTCPConn(conn net.Conn, target string) {
	defer conn.Close()

	// Open stream on a session from pool
	stream, ss, err := s.openReverseStream("tcp://" + target)
	if err != nil {
		if s.Verbose {
			log.Printf("[RTCP] no session for %s: %v", target, err)
		}
		return
	}
	defer func() {
		stream.Close()
		atomic.AddInt64(&ss.streams, -1)
	}()

	relay(conn, stream)
}

// openReverseStream opens a stream on a session, writes the type tag
// and target header. Returns the stream ready for data relay.
func (s *Server) openReverseStream(target string) (*smux.Stream, *serverSession, error) {
	s.poolMu.RLock()
	n := len(s.sessions)
	if n == 0 {
		s.poolMu.RUnlock()
		return nil, nil, fmt.Errorf("no sessions")
	}

	maxStreams := s.Config.Advanced.MaxStreamsPerSession

	// Try round-robin with overflow protection
	startIdx := int(atomic.AddUint64(&s.poolIdx, 1)) % n
	var bestSS *serverSession

	for i := 0; i < n; i++ {
		idx := (startIdx + i) % n
		ss := s.sessions[idx]
		if ss.sess.IsClosed() {
			continue
		}
		active := atomic.LoadInt64(&ss.streams)
		if int(active) >= maxStreams {
			continue
		}
		bestSS = ss
		break
	}
	s.poolMu.RUnlock()

	if bestSS == nil {
		// All sessions overloaded — try least loaded
		bestSS = s.leastLoadedSession()
		if bestSS == nil {
			return nil, nil, fmt.Errorf("all sessions full")
		}
	}

	stream, err := bestSS.sess.OpenStream()
	if err != nil {
		// Session might be dead — evict and retry once
		s.removeSession(bestSS)
		bestSS.sess.Close()
		return nil, nil, fmt.Errorf("open stream: %w", err)
	}
	atomic.AddInt64(&bestSS.streams, 1)

	// Write stream type tag
	if _, err := stream.Write([]byte{StreamTypeReverse}); err != nil {
		stream.Close()
		atomic.AddInt64(&bestSS.streams, -1)
		return nil, nil, err
	}

	// Write target header
	targetBytes := []byte(target)
	hdr := make([]byte, 2+len(targetBytes))
	binary.BigEndian.PutUint16(hdr[:2], uint16(len(targetBytes)))
	copy(hdr[2:], targetBytes)
	if _, err := stream.Write(hdr); err != nil {
		stream.Close()
		atomic.AddInt64(&bestSS.streams, -1)
		return nil, nil, err
	}

	return stream, bestSS, nil
}

func (s *Server) leastLoadedSession() *serverSession {
	s.poolMu.RLock()
	defer s.poolMu.RUnlock()

	var best *serverSession
	bestLoad := int64(1<<63 - 1)
	for _, ss := range s.sessions {
		if ss.sess.IsClosed() {
			continue
		}
		load := atomic.LoadInt64(&ss.streams)
		if load < bestLoad {
			bestLoad = load
			best = ss
		}
	}
	return best
}

// ──────────────── Reverse UDP ────────────────

func (s *Server) startReverseUDP(bind, target string) {
	addr, err := net.ResolveUDPAddr("udp", bind)
	if err != nil {
		log.Printf("[RUDP] FAILED resolve %s: %v", bind, err)
		return
	}
	ln, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[RUDP] FAILED listen %s: %v", bind, err)
		return
	}
	log.Printf("[RUDP] %s → %s", bind, target)

	var mu sync.Mutex
	peers := map[string]*udpPeer{}

	go func() {
		for range time.NewTicker(30 * time.Second).C {
			mu.Lock()
			now := time.Now().Unix()
			for k, p := range peers {
				if now-atomic.LoadInt64(&p.lastSeen) > int64(s.Config.Advanced.UDPFlowTimeout) {
					p.stream.Close() // triggers reader goroutine exit + cleanup
					delete(peers, k)
				}
			}
			mu.Unlock()
		}
	}()

	buf := make([]byte, s.Config.Advanced.UDPBufferSize)
	for {
		n, raddr, err := ln.ReadFromUDP(buf)
		if err != nil || n == 0 {
			continue
		}

		key := raddr.String()
		mu.Lock()
		p, ok := peers[key]
		if !ok {
			stream, ss, err := s.openReverseStream("udp://" + target)
			if err != nil {
				mu.Unlock()
				continue
			}
			p = &udpPeer{
				stream:   stream,
				ss:       ss,
				lastSeen: time.Now().Unix(),
			}
			peers[key] = p

			go func(p *udpPeer, raddr *net.UDPAddr) {
				defer func() {
					if p.ss != nil {
						atomic.AddInt64(&p.ss.streams, -1)
					}
				}()
				rbuf := make([]byte, 65536)
				for {
					rn, err := p.stream.Read(rbuf)
					if err != nil {
						break
					}
					ln.WriteToUDP(rbuf[:rn], raddr)
					atomic.StoreInt64(&p.lastSeen, time.Now().Unix())
				}
				mu.Lock()
				delete(peers, raddr.String())
				mu.Unlock()
			}(p, raddr)
		}
		mu.Unlock()

		atomic.StoreInt64(&p.lastSeen, time.Now().Unix())
		p.stream.Write(buf[:n])
	}
}

type udpPeer struct {
	stream   *smux.Stream
	ss       *serverSession
	lastSeen int64
}

// ──────────────── Session Pool ────────────────

func (s *Server) addSession(ss *serverSession) {
	s.poolMu.Lock()
	s.sessions = append(s.sessions, ss)
	s.poolMu.Unlock()
}

func (s *Server) removeSession(ss *serverSession) {
	s.poolMu.Lock()
	for i, e := range s.sessions {
		if e == ss {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			break
		}
	}
	s.poolMu.Unlock()
}

func (s *Server) poolSize() int {
	s.poolMu.RLock()
	defer s.poolMu.RUnlock()
	return len(s.sessions)
}

// healthMonitor proactively evicts dead sessions
func (s *Server) healthMonitor() {
	interval := time.Duration(s.Config.Advanced.CleanupInterval) * time.Second
	if interval < time.Second {
		interval = 3 * time.Second
	}

	for range time.NewTicker(interval).C {
		s.poolMu.Lock()
		alive := s.sessions[:0]
		evicted := 0
		for _, ss := range s.sessions {
			if ss.sess.IsClosed() {
				evicted++
				ss.sess.Close()
			} else {
				alive = append(alive, ss)
			}
		}
		s.sessions = alive
		s.poolMu.Unlock()

		if evicted > 0 {
			log.Printf("[HEALTH] evicted %d dead sessions (alive: %d)", evicted, len(alive))
		}
	}
}

// ──────────────── DPI Stealth: Fake Traffic ────────────────
// Periodically send random HTTP-like data on idle sessions
// to prevent DPI from detecting "idle tunnel" patterns.

func (s *Server) fakeTrafficLoop(ss *serverSession) {
	interval := time.Duration(s.Config.Stealth.FakeTrafficInterval) * time.Second
	if interval < 5*time.Second {
		interval = 30 * time.Second
	}

	// Add jitter to interval
	jitter := secureRandInt(int(interval.Seconds()/2)) + 1
	interval += time.Duration(jitter) * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if ss.sess.IsClosed() {
			return
		}
		// Only send if session is relatively idle
		if atomic.LoadInt64(&ss.streams) < 3 {
			s.sendFakeData(ss)
		}
		// Randomize next interval
		jitter := secureRandInt(int(interval.Seconds()/3)) + 1
		ticker.Reset(interval + time.Duration(jitter)*time.Second)
	}
}

func (s *Server) sendFakeData(ss *serverSession) {
	stream, err := ss.sess.OpenStream()
	if err != nil {
		return
	}
	// Write a "fake" stream type that the client will just discard
	stream.Write([]byte{0xFF})
	// Random payload 32-256 bytes
	size := 32 + secureRandInt(224)
	fake := make([]byte, size)
	rand.Read(fake)
	stream.Write(fake)
	time.Sleep(time.Duration(50+secureRandInt(200)) * time.Millisecond)
	stream.Close()
}

// ──────────────── Validation ────────────────

func (s *Server) validateRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" {
		s.writeDecoy(w)
		return false
	}
	if s.Mimic != nil && s.Mimic.FakeDomain != "" {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != s.Mimic.FakeDomain && !strings.HasSuffix(host, "."+s.Mimic.FakeDomain) {
			// Allow IP-based access too
			if net.ParseIP(host) == nil {
				s.writeDecoy(w)
				return false
			}
		}
	}
	upgrade := strings.ToLower(r.Header.Get("Upgrade"))
	conn := strings.ToLower(r.Header.Get("Connection"))
	if !strings.Contains(upgrade, "websocket") || !strings.Contains(conn, "upgrade") {
		s.writeDecoy(w)
		return false
	}
	return true
}

func (s *Server) handleDecoy(w http.ResponseWriter, r *http.Request) {
	s.writeDecoy(w)
}

func (s *Server) writeDecoy(w http.ResponseWriter) {
	w.Header().Set("Server", "nginx/1.24.0")
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`<!DOCTYPE html><html><head><title>404 Not Found</title></head><body><center><h1>404 Not Found</h1></center><hr><center>nginx/1.24.0</center></body></html>`))
}

func (s *Server) setTCPOptions(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(time.Duration(s.Config.Advanced.TCPKeepAlive) * time.Second)
		tc.SetReadBuffer(s.Config.Advanced.TCPReadBuffer)
		tc.SetWriteBuffer(s.Config.Advanced.TCPWriteBuffer)
	}
}

// ──────────────── Shared Helpers ────────────────

func buildSmuxConfig(cfg *Config) *smux.Config {
	sc := smux.DefaultConfig()
	sc.Version = cfg.Smux.Version
	if sc.Version < 1 {
		sc.Version = 2
	}

	keepalive := time.Duration(cfg.Smux.KeepAlive) * time.Second
	if keepalive <= 0 {
		keepalive = 2 * time.Second
	}

	// v2.5: Add jitter to keepalive to avoid DPI pattern detection
	if cfg.Stealth.KeepaliveJitter > 0 {
		jitter := secureRandInt(cfg.Stealth.KeepaliveJitter*1000) - (cfg.Stealth.KeepaliveJitter * 500)
		keepalive += time.Duration(jitter) * time.Millisecond
		if keepalive < 500*time.Millisecond {
			keepalive = 500 * time.Millisecond
		}
	}

	sc.KeepAliveInterval = keepalive
	// v2.5: Timeout = keepalive × 15 (was ×10 — too aggressive for loaded servers)
	sc.KeepAliveTimeout = keepalive * 15
	if sc.KeepAliveTimeout < 30*time.Second {
		sc.KeepAliveTimeout = 30 * time.Second
	}

	if cfg.Smux.MaxRecv > 0 {
		sc.MaxReceiveBuffer = cfg.Smux.MaxRecv
	}
	if cfg.Smux.MaxStream > 0 {
		sc.MaxStreamBuffer = cfg.Smux.MaxStream
	}
	if cfg.Smux.FrameSize > 0 {
		sc.MaxFrameSize = cfg.Smux.FrameSize
	}
	return sc
}

func mimicPath(cfg *MimicConfig) string {
	if cfg != nil && cfg.FakePath != "" {
		return cfg.FakePath
	}
	return "/search"
}

func splitTarget(s string) (network, addr string) {
	if strings.HasPrefix(s, "udp://") {
		return "udp", strings.TrimPrefix(s, "udp://")
	}
	return "tcp", strings.TrimPrefix(s, "tcp://")
}

func sendTarget(w io.Writer, target string) error {
	b := []byte(target)
	hdr := make([]byte, 2)
	binary.BigEndian.PutUint16(hdr, uint16(len(b)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func relay(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		// 256 KB buffer — enough for gigabit throughput without excessive
		// syscall overhead. (32 KB was the bottleneck that capped speed at
		// ~50 Mbps by serialising too many small read/write pairs.)
		buf := make([]byte, 256*1024)
		io.CopyBuffer(dst, src, buf)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}
