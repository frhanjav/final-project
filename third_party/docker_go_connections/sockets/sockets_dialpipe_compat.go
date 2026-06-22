//go:build !windows

package sockets

import (
	"errors"
	"net"
	"time"
)

// DialPipe is kept for older Docker client codepaths that still reference it.
// Non-Windows builds never use named pipes, so this stub only preserves
// compile-time compatibility.
func DialPipe(_ string, _ time.Duration) (net.Conn, error) {
	return nil, errors.New("named pipes are only supported on Windows")
}
