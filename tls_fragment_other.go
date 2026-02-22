//go:build !linux

package httpmux

import (
	"fmt"
	"net"
	"time"
)

// dialRawTCP on non-Linux: falls back to standard dial.
// TCP_NODELAY will be set after connection in the calling code.
func dialRawTCP(addr string, timeout time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("raw TCP dial not supported on this platform")
}
