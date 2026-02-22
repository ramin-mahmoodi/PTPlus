package httpmux

import (
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"
)

// ═══════════════════════════════════════════════════════════════
// TLS ClientHello Fragmentation (anti-DPI)
//
// v2.5 changes:
//   • Unified setTCPNoDelay (single definition, bool param)
//   • Better fragment randomization
//   • Multi-fragment support for large ClientHellos
// ═══════════════════════════════════════════════════════════════

type FragmentConfig struct {
	Enabled  bool `yaml:"enabled"`
	MinSize  int  `yaml:"min_size"`
	MaxSize  int  `yaml:"max_size"`
	MinDelay int  `yaml:"min_delay"`
	MaxDelay int  `yaml:"max_delay"`
}

func DefaultFragmentConfig() FragmentConfig {
	return FragmentConfig{
		Enabled:  true,
		MinSize:  64,
		MaxSize:  191,
		MinDelay: 1,
		MaxDelay: 3,
	}
}

// FragmentedConn wraps a net.Conn and fragments the first large write.
type FragmentedConn struct {
	net.Conn
	fragmentSize int
	delay        time.Duration
	firstWrite   bool
	mu           sync.Mutex
}

func (c *FragmentedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.firstWrite || len(b) <= c.fragmentSize {
		c.firstWrite = true
		return c.Conn.Write(b)
	}

	c.firstWrite = true

	// v2.5: Multi-fragment — split at random boundary
	frag1 := b[:c.fragmentSize]
	frag2 := b[c.fragmentSize:]

	n1, err := c.Conn.Write(frag1)
	if err != nil {
		return n1, err
	}

	// Random delay between fragments
	delay := c.delay + time.Duration(secureRandInt(2))*time.Millisecond
	time.Sleep(delay)

	n2, err := c.Conn.Write(frag2)
	return n1 + n2, err
}

// DialFragmented creates a TCP connection with ClientHello fragmentation.
func DialFragmented(addr string, cfg *FragmentConfig, timeout time.Duration) (net.Conn, error) {
	if cfg == nil || !cfg.Enabled {
		return net.DialTimeout("tcp", addr, timeout)
	}

	minSize := cfg.MinSize
	maxSize := cfg.MaxSize
	minDelay := cfg.MinDelay
	maxDelay := cfg.MaxDelay
	if minSize <= 0 {
		minSize = 64
	}
	if maxSize <= 0 {
		maxSize = 191
	}
	if minDelay <= 0 {
		minDelay = 1
	}
	if maxDelay <= 0 {
		maxDelay = 3
	}

	fragSize := minSize
	diff := maxSize - minSize
	if diff > 0 {
		fragSize += secureRandInt(diff + 1)
	}

	delayMs := minDelay
	delayDiff := maxDelay - minDelay
	if delayDiff > 0 {
		delayMs += secureRandInt(delayDiff + 1)
	}
	delay := time.Duration(delayMs) * time.Millisecond

	// Try raw socket first (TCP_NODELAY before connect)
	conn, err := dialRawTCP(addr, timeout)
	if err != nil {
		conn, err = net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return nil, fmt.Errorf("dial: %w", err)
		}
		setTCPNoDelay(conn, true)
	}

	return &FragmentedConn{
		Conn:         conn,
		fragmentSize: fragSize,
		delay:        delay,
		firstWrite:   false,
	}, nil
}

// setTCPNoDelay — single definition for entire package.
// v2.5: Takes bool param for enable/disable.
func setTCPNoDelay(conn net.Conn, enable bool) {
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(enable)
	}
}

// ──────────────────────────────────────────────────
// Random cipher suites
// ──────────────────────────────────────────────────

var picotunCipherSuites = []uint16{
	0xc02f, // TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
	0xc030, // TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
	0xcca8, // TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256
	0xcca9, // TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
	0xc02b, // TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
	0xc02c, // TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
}

func GetRandomCipherSuites() []uint16 {
	suites := make([]uint16, len(picotunCipherSuites))
	copy(suites, picotunCipherSuites)

	for i := len(suites) - 1; i > 0; i-- {
		j := secureRandInt(i + 1)
		suites[i], suites[j] = suites[j], suites[i]
	}

	count := secureRandInt(4) + 3
	if count > len(suites) {
		count = len(suites)
	}
	return suites[:count]
}

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

func randomFragmentSize() int {
	b := make([]byte, 1)
	rand.Read(b)
	return int(b[0]&0x7f) + 64
}
