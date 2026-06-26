package ipc

import "testing"

func TestValidatePeerAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		userID  int64
		peerUID int
		wantErr bool
	}{
		{name: "zero user_id skips check", userID: 0, peerUID: 501, wantErr: false},
		{name: "matching uid", userID: 501, peerUID: 501, wantErr: false},
		{name: "mismatched uid", userID: 1000, peerUID: 501, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePeerAuth(IPCMessage{UserID: tt.userID}, tt.peerUID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePeerAuth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}