//go:build linux

package httpmux

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// dialRawTCP creates a TCP socket with TCP_NODELAY set BEFORE connecting.
// This is better than setting TCP_NODELAY after connect because even the
// SYN-ACK exchange is not affected by Nagle's algorithm.
// Matches PicoTun's dialWithFragmentation raw socket approach.
func dialRawTCP(addr string, timeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Resolve
	tcpAddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	// Only IPv4 for raw socket; IPv6 falls back
	ip4 := tcpAddr.IP.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("ipv6 not supported for raw dial")
	}

	// Create socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	// TCP_NODELAY before connect (like PicoTun)
	syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

	// Build sockaddr
	sa := &syscall.SockaddrInet4{Port: tcpAddr.Port}
	copy(sa.Addr[:], ip4)

	// Non-blocking connect with timeout
	syscall.SetNonblock(fd, true)
	err = syscall.Connect(fd, sa)

	if err == syscall.EINPROGRESS {
		// Wait for connect using epoll
		if err := waitConnect(fd, timeout); err != nil {
			syscall.Close(fd)
			return nil, err
		}
	} else if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("connect: %w", err)
	}

	// Back to blocking
	syscall.SetNonblock(fd, false)

	// fd → os.File → net.Conn (like PicoTun)
	f := os.NewFile(uintptr(fd), "tcp-frag")
	conn, err := net.FileConn(f)
	f.Close() // FileConn dups the fd, so close the original file
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("file conn: %w", err)
	}

	return conn, nil
}

// waitConnect waits for a non-blocking connect to complete using epoll.
func waitConnect(fd int, timeout time.Duration) error {
	epfd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		return fmt.Errorf("epoll_create: %w", err)
	}
	defer syscall.Close(epfd)

	event := syscall.EpollEvent{
		Events: syscall.EPOLLOUT | syscall.EPOLLERR | syscall.EPOLLHUP,
		Fd:     int32(fd),
	}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		return fmt.Errorf("epoll_ctl: %w", err)
	}

	events := make([]syscall.EpollEvent, 1)
	msTimeout := int(timeout.Milliseconds())
	if msTimeout <= 0 {
		msTimeout = 10000 // 10s default
	}

	n, err := syscall.EpollWait(epfd, events, msTimeout)
	if err != nil {
		return fmt.Errorf("epoll_wait: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("connect timed out")
	}

	// Check for socket error
	val, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
	if err != nil {
		return fmt.Errorf("getsockopt: %w", err)
	}
	if val != 0 {
		return fmt.Errorf("connect error: %s", syscall.Errno(val))
	}

	return nil
}
