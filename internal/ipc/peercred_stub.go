//go:build !linux && !darwin

package ipc

import (
	"errors"
	"net"
)

var errPeerCredUnsupported = errors.New("peer credential lookup unsupported on this platform")

func connPeerUID(_ net.Conn) (int, error) {
	return 0, errPeerCredUnsupported
}