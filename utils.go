package httpmux

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// RandString returns a URL-safe random string with ~n chars.
func RandString(n int) string {
	if n <= 0 {
		return ""
	}
	// base64 expands by ~4/3, so generate enough bytes
	b := make([]byte, (n*3+3)/4)
	_, _ = rand.Read(b)
	s := base64.RawURLEncoding.EncodeToString(b)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SplitMap parses "bind->target".
// bind can be "1412" or "0.0.0.0:1412"
func SplitMap(s string) (bind string, target string, ok bool) {
	parts := strings.Split(s, "->")
	if len(parts) != 2 {
		return "", "", false
	}
	bind = strings.TrimSpace(parts[0])
	target = strings.TrimSpace(parts[1])

	if bind == "" || target == "" {
		return "", "", false
	}
	if !strings.Contains(bind, ":") {
		bind = "0.0.0.0:" + bind
	}
	return bind, target, true
}
