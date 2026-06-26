//go:build darwin

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func connPeerUID(conn net.Conn) (int, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("not a unix connection")
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("syscall conn: %w", err)
	}

	var cred *unix.Xucred
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, err = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	})
	if ctrlErr != nil {
		return 0, fmt.Errorf("control: %w", ctrlErr)
	}
	if err != nil {
		return 0, fmt.Errorf("getsockopt LOCAL_PEERCRED: %w", err)
	}
	return int(cred.Uid), nil
}