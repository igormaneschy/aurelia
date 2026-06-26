package ipc

import "fmt"

// validatePeerAuth ensures a client-supplied user_id matches the Unix UID of
// the connected peer process. Session-management messages omit user_id (0) and
// skip this check; the daemon resolves identity downstream.
func validatePeerAuth(msg IPCMessage, peerUID int) error {
	if msg.UserID == 0 {
		return nil
	}
	if int(msg.UserID) != peerUID {
		return fmt.Errorf("user_id %d does not match peer uid %d", msg.UserID, peerUID)
	}
	return nil
}