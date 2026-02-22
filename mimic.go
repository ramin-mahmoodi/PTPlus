package httpmux

import (
	"bufio"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type MimicConfig struct {
	FakeDomain    string   `yaml:"fake_domain"`
	FakePath      string   `yaml:"fake_path"`
	UserAgent     string   `yaml:"user_agent"`
	CustomHeaders []string `yaml:"custom_headers"`
	SessionCookie bool     `yaml:"session_cookie"`
	Chunked       bool     `yaml:"chunked"`
}

// ═══════════════════════════════════════════════════════════════
// bufferedConn — CRITICAL FIX for data loss bug.
//
// Problem: bufio.NewReader(conn) in http.ReadResponse may read
// ahead beyond the HTTP response boundary. Those extra bytes are
// the first smux frames (keepalive, version negotiation).
// If we discard the bufio.Reader and use raw conn for EncryptedConn,
// those buffered bytes are LOST → smux session dies in ~30 seconds.
//
// Solution: wrap conn + bufio.Reader so Read() goes through the
// buffer first, preserving any pre-read smux data.
// ═══════════════════════════════════════════════════════════════

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

// ClientHandshake performs the HTTP upgrade handshake (client side).
// Returns a wrapped net.Conn that preserves any buffered data.
func ClientHandshake(conn net.Conn, cfg *MimicConfig) (net.Conn, error) {
	domain := "www.google.com"
	path := "/"
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

	if cfg != nil {
		if cfg.FakeDomain != "" {
			domain = cfg.FakeDomain
		}
		if cfg.FakePath != "" {
			path = cfg.FakePath
		}
		if cfg.UserAgent != "" {
			ua = cfg.UserAgent
		}
	}

	fullURL := "http://" + domain + path
	if strings.Contains(path, "{rand}") {
		fullURL, _ = BuildURLWithFakePath("http://"+domain, path)
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Host", domain)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", generateWebSocketKey())
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	if cfg != nil {
		for _, h := range cfg.CustomHeaders {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
		if cfg.SessionCookie {
			req.AddCookie(&http.Cookie{Name: "session", Value: generateSessionID()})
		}
	}

	reqDump, err := httputil.DumpRequest(req, false)
	if err != nil {
		return nil, err
	}
	if _, err = conn.Write(reqDump); err != nil {
		return nil, err
	}

	// ── Read response using bufio.Reader ──
	// CRITICAL: Keep the bufio.Reader — it may contain pre-read smux data!
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 101 && resp.StatusCode != 200 {
		return nil, fmt.Errorf("handshake: expected 101, got %d", resp.StatusCode)
	}

	// Return wrapped conn that reads through bufio first
	return &bufferedConn{Conn: conn, r: br}, nil
}

// ServerHandshake — server-side validation (for tcpmux direct mode)
func ServerHandshake(conn net.Conn, cfg *MimicConfig) error {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return err
	}

	if cfg != nil && cfg.FakeDomain != "" {
		if req.Host != cfg.FakeDomain && !strings.HasSuffix(req.Host, "."+cfg.FakeDomain) {
			writeFakeResponse(conn, 404)
			return fmt.Errorf("invalid host: %s", req.Host)
		}
	}

	expectedPath := "/"
	if cfg != nil && cfg.FakePath != "" {
		expectedPath = strings.Split(cfg.FakePath, "{")[0]
	}
	if !strings.HasPrefix(req.URL.Path, expectedPath) {
		writeFakeResponse(conn, 404)
		return fmt.Errorf("invalid path: %s", req.URL.Path)
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n" +
		"\r\n"
	_, err = conn.Write([]byte(resp))
	return err
}

func writeFakeResponse(conn net.Conn, code int) {
	resp := fmt.Sprintf("HTTP/1.1 %d Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code)
	conn.Write([]byte(resp))
}

func ApplyMimicHeaders(req *http.Request, cfg *MimicConfig, cookieName, cookieValue string) {
	if cfg == nil {
		return
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	if cfg.FakeDomain != "" {
		req.Header.Set("Host", cfg.FakeDomain)
	}
}

func BuildURLWithFakePath(baseURL, fakePath string) (string, error) {
	if fakePath == "" {
		return baseURL, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	fp := fakePath
	if strings.Contains(fp, "{rand}") {
		fp = strings.ReplaceAll(fp, "{rand}", randAlphaNum(8))
	}
	if !strings.HasPrefix(fp, "/") {
		fp = "/" + fp
	}
	u.Path = fp
	return u.String(), nil
}

func randAlphaNum(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func generateWebSocketKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
