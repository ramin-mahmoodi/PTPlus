package httpmux

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"
)

// ═══════════════════════════════════════════════════════════════
// EncryptedConn v2.5 — per-packet AES-256-GCM encryption
//
// Changes from v2.4:
//   • Burst splitting — large writes split into random chunks
//   • Better padding — variable size per packet (anti traffic analysis)
//   • No delay on small packets (keepalive/control safe)
// ═══════════════════════════════════════════════════════════════

type EncryptedConn struct {
	conn    net.Conn
	gcm     cipher.AEAD
	obfs    *ObfsConfig
	stealth *StealthConfig

	readMu  sync.Mutex
	writeMu sync.Mutex
	readBuf []byte
}

func NewEncryptedConn(conn net.Conn, psk string, obfs *ObfsConfig, stealth ...*StealthConfig) (*EncryptedConn, error) {
	ec := &EncryptedConn{conn: conn, obfs: obfs}
	if len(stealth) > 0 && stealth[0] != nil {
		ec.stealth = stealth[0]
	}

	if psk == "" {
		return ec, nil
	}

	hash := sha256.Sum256([]byte(psk))
	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	ec.gcm = gcm
	return ec, nil
}

// SetStealth enables v2.5 DPI stealth features
func (c *EncryptedConn) SetStealth(s *StealthConfig) {
	c.stealth = s
}

// ──────────────────── Write ────────────────────

func (c *EncryptedConn) Write(data []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// v2.5: Burst split — break large writes into random-sized chunks
	// This prevents DPI from seeing consistent packet size patterns.
	if c.stealth != nil && c.stealth.BurstSplit && len(data) > c.stealth.MaxBurstSize {
		return c.burstWrite(data)
	}

	return c.writePacket(data)
}

func (c *EncryptedConn) writePacket(data []byte) (int, error) {
	payload := data

	// ① Padding BEFORE encryption
	if c.obfs != nil && c.obfs.Enabled {
		payload = addPadding(data, c.obfs)
	} else if c.stealth != nil && c.stealth.RandomPadding && len(data) > 4 {
		// v2.5: Stealth padding even without full obfuscation
		payload = addStealthPadding(data, c.stealth)
	}

	// ② Encrypt
	if c.gcm != nil {
		nonce := make([]byte, c.gcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return 0, fmt.Errorf("nonce: %w", err)
		}
		ciphertext := c.gcm.Seal(nil, nonce, payload, nil)

		pktLen := len(nonce) + len(ciphertext)
		buf := make([]byte, 4+pktLen)
		binary.BigEndian.PutUint32(buf[:4], uint32(pktLen))
		copy(buf[4:], nonce)
		copy(buf[4+len(nonce):], ciphertext)

		if _, err := c.conn.Write(buf); err != nil {
			return 0, err
		}
	} else {
		buf := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(buf[:4], uint32(len(payload)))
		copy(buf[4:], payload)

		if _, err := c.conn.Write(buf); err != nil {
			return 0, err
		}
	}

	// ③ Timing jitter only for large data (protect keepalives)
	if c.obfs != nil && c.obfs.Enabled && c.obfs.MaxDelayMS > 0 && len(data) > 128 {
		obfsDelay(c.obfs)
	}

	return len(data), nil
}

// burstWrite splits a large write into random-sized chunks
func (c *EncryptedConn) burstWrite(data []byte) (int, error) {
	total := 0
	remaining := data
	maxBurst := c.stealth.MaxBurstSize
	if maxBurst <= 0 {
		maxBurst = 4096
	}

	for len(remaining) > 0 {
		// Random chunk size: 512 to maxBurst bytes
		chunkSize := 512 + secureRandInt(maxBurst-512+1)
		if chunkSize > len(remaining) {
			chunkSize = len(remaining)
		}
		n, err := c.writePacket(remaining[:chunkSize])
		total += n
		if err != nil {
			return total, err
		}
		remaining = remaining[chunkSize:]

		// Tiny delay between chunks (looks like HTTP chunked transfer)
		if len(remaining) > 0 {
			time.Sleep(time.Duration(1+secureRandInt(5)) * time.Millisecond)
		}
	}
	return total, nil
}

// ──────────────────── Read ────────────────────

func (c *EncryptedConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if len(c.readBuf) > 0 {
		n := copy(p, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, err
	}
	pktLen := binary.BigEndian.Uint32(header)
	if pktLen == 0 || pktLen > 16<<20 {
		return 0, fmt.Errorf("invalid packet length: %d", pktLen)
	}

	pkt := make([]byte, pktLen)
	if _, err := io.ReadFull(c.conn, pkt); err != nil {
		return 0, err
	}

	var plaintext []byte
	if c.gcm != nil {
		ns := c.gcm.NonceSize()
		if int(pktLen) < ns {
			return 0, fmt.Errorf("packet too short")
		}
		var err error
		plaintext, err = c.gcm.Open(nil, pkt[:ns], pkt[ns:], nil)
		if err != nil {
			return 0, fmt.Errorf("decrypt: %w", err)
		}
	} else {
		plaintext = pkt
	}

	// Remove padding
	if c.obfs != nil && c.obfs.Enabled {
		plaintext = removePadding(plaintext)
		if plaintext == nil {
			return 0, fmt.Errorf("invalid padding")
		}
	} else if c.stealth != nil && c.stealth.RandomPadding {
		stripped := removeStealthPadding(plaintext)
		if stripped != nil {
			plaintext = stripped
		}
		// If strip fails, use raw plaintext (backward compat)
	}

	n := copy(p, plaintext)
	if n < len(plaintext) {
		c.readBuf = make([]byte, len(plaintext)-n)
		copy(c.readBuf, plaintext[n:])
	}
	return n, nil
}

// ──────────────────── Padding ────────────────────

func addPadding(data []byte, obfs *ObfsConfig) []byte {
	padLen := obfs.MinPadding
	diff := obfs.MaxPadding - obfs.MinPadding
	if diff > 0 {
		padLen += secureRandInt(diff)
	}
	out := make([]byte, 2+len(data)+padLen)
	binary.BigEndian.PutUint16(out[:2], uint16(len(data)))
	copy(out[2:], data)
	if padLen > 0 {
		rand.Read(out[2+len(data):])
	}
	return out
}

func removePadding(data []byte) []byte {
	if len(data) < 2 {
		return nil
	}
	origLen := binary.BigEndian.Uint16(data[:2])
	if int(origLen)+2 > len(data) {
		return nil
	}
	return data[2 : 2+origLen]
}

// v2.5: Stealth padding — same format as obfs padding but uses stealth config
func addStealthPadding(data []byte, s *StealthConfig) []byte {
	padLen := s.MinPadding + secureRandInt(s.MaxPadding-s.MinPadding+1)
	out := make([]byte, 2+len(data)+padLen)
	binary.BigEndian.PutUint16(out[:2], uint16(len(data)))
	copy(out[2:], data)
	if padLen > 0 {
		rand.Read(out[2+len(data):])
	}
	return out
}

func removeStealthPadding(data []byte) []byte {
	if len(data) < 2 {
		return nil
	}
	origLen := binary.BigEndian.Uint16(data[:2])
	if int(origLen)+2 > len(data) || origLen == 0 {
		return nil
	}
	return data[2 : 2+origLen]
}

// ──────────────────── Traffic timing ────────────────────

func obfsDelay(obfs *ObfsConfig) {
	min := obfs.MinDelayMS
	max := obfs.MaxDelayMS
	if max <= min || max <= 0 {
		return
	}
	d := min + secureRandInt(max-min)
	if d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
}

// ──────────────────── net.Conn interface ────────────────────

func (c *EncryptedConn) Close() error                       { return c.conn.Close() }
func (c *EncryptedConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *EncryptedConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *EncryptedConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *EncryptedConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *EncryptedConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

var _ net.Conn = (*EncryptedConn)(nil)

// ──────────────────── Crypto-safe random ────────────────────

func secureRandInt(n int) int {
	if n <= 0 {
		return 0
	}
	val, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(val.Int64())
}
