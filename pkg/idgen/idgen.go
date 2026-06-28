package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a random 128-bit identifier as lowercase hex (32 chars).
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("idgen: %v", err))
	}
	return hex.EncodeToString(b[:])
}