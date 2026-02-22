package httpmux

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

type ObfsConfig struct {
	Enabled     bool `yaml:"enabled"`
	MinPadding  int  `yaml:"min_padding"`
	MaxPadding  int  `yaml:"max_padding"`
	MinDelayMS  int  `yaml:"min_delay_ms"`
	MaxDelayMS  int  `yaml:"max_delay_ms"`
	BurstChance int  `yaml:"burst_chance"`
}

// Decoy strings to inject into the padding to mimic PicoTun's traffic patterns
var decoyPatterns = []string{
	"User-Agent: ",
	"GET / HTTP/1.1",
	"POST / HTTP/1.1",
	"Host: ",
	"Accept: */*",
	"Content-Type: application/octet-stream",
	"Connection: keep-alive",
	"Cache-Control: no-cache",
}

// Wire: [2 bytes padLen][data][padding]
func ApplyObfuscation(data []byte, cfg *ObfsConfig) []byte {
	if cfg == nil || !cfg.Enabled {
		return data
	}

	pad := cfg.MinPadding
	if cfg.MaxPadding > cfg.MinPadding {
		pad = cfg.MinPadding + int(randByte())%(cfg.MaxPadding-cfg.MinPadding+1)
	}
	if pad < 0 {
		pad = 0
	}

	out := make([]byte, 2+len(data)+pad)
	binary.BigEndian.PutUint16(out[:2], uint16(pad))
	copy(out[2:], data)

	if pad > 0 {
		paddingArea := out[2+len(data):]
		_, _ = rand.Read(paddingArea)

		// PicoTun-Mimic: Inject a decoy string if padding is large enough
		if pad > 12 {
			decoyIdx := int(randByte()) % len(decoyPatterns)
			decoyStr := decoyPatterns[decoyIdx]

			if len(decoyStr) < pad {
				maxOffset := pad - len(decoyStr)
				offset := int(randByte()) % (maxOffset + 1)
				copy(paddingArea[offset:], []byte(decoyStr))
			}
		}
	}
	return out
}

func StripObfuscation(data []byte, cfg *ObfsConfig) []byte {
	if cfg == nil || !cfg.Enabled {
		return data
	}
	if len(data) < 2 {
		return nil
	}
	pad := int(binary.BigEndian.Uint16(data[:2]))
	body := data[2:]
	if pad < 0 || pad > len(body) {
		return nil
	}
	return body[:len(body)-pad]
}

func ApplyDelay(cfg *ObfsConfig) {
	if cfg == nil || !cfg.Enabled {
		return
	}
	if cfg.MaxDelayMS <= 0 {
		return
	}
	min := cfg.MinDelayMS
	max := cfg.MaxDelayMS
	if max < min {
		max = min
	}
	d := min
	if max > min {
		d = min + int(randByte())%(max-min+1)
	}
	time.Sleep(time.Duration(d) * time.Millisecond)
}

func randByte() byte {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return b[0]
}