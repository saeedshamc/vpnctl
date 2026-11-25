//go:build linux

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// PeerCred is the SO_PEERCRED identity of the connecting process.
type PeerCred struct {
	PID int
	UID uint32
	GID uint32
}

// PeerCredentials returns the credentials of a unix-socket peer.
func PeerCredentials(conn *net.UnixConn) (PeerCred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return PeerCred{}, err
	}
	var (
		cred PeerCred
		opErr error
	)
	err = raw.Control(func(fd uintptr) {
		ucred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			opErr = e
			return
		}
		cred = PeerCred{PID: int(ucred.Pid), UID: ucred.Uid, GID: ucred.Gid}
	})
	if err != nil {
		return PeerCred{}, err
	}
	if opErr != nil {
		return PeerCred{}, opErr
	}
	return cred, nil
}

// AllowPeer reports whether the peer may command the helper.
// Root (uid 0) and the daemon's own uid are always allowed; additionally
// any member of group "vpnctl" (resolved at listen time via AllowedGIDs).
func AllowPeer(cred PeerCred, allowedUIDs []uint32, allowedGIDs []uint32) bool {
	for _, u := range allowedUIDs {
		if cred.UID == u {
			return true
		}
	}
	for _, g := range allowedGIDs {
		if cred.GID == g {
			return true
		}
	}
	return false
}

// DescribeCred is a short log line for a peer.
func DescribeCred(c PeerCred) string {
	return fmt.Sprintf("pid=%d uid=%d gid=%d", c.PID, c.UID, c.GID)
}
