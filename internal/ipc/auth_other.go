//go:build !linux

package ipc

import (
	"fmt"
	"net"
)

// PeerCred is a stub on non-Linux (dev hosts). Auth is skipped.
type PeerCred struct {
	PID int
	UID uint32
	GID uint32
}

// PeerCredentials returns a synthetic local peer on non-Linux.
func PeerCredentials(conn *net.UnixConn) (PeerCred, error) {
	_ = conn
	return PeerCred{PID: 1, UID: 0, GID: 0}, nil
}

// AllowPeer always returns true on non-Linux (development only).
func AllowPeer(cred PeerCred, allowedUIDs []uint32, allowedGIDs []uint32) bool {
	_, _, _ = cred, allowedUIDs, allowedGIDs
	return true
}

// DescribeCred is a short log line for a peer.
func DescribeCred(c PeerCred) string {
	return fmt.Sprintf("pid=%d uid=%d gid=%d", c.PID, c.UID, c.GID)
}
