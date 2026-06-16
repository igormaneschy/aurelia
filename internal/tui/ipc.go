package tui

import (
	"github.com/igormaneschy/aurelia/internal/ipc"
)

// DefaultSocketPath returns the default path to the IPC socket.
// Wraps ipc.DefaultSocketPath for convenience.
var DefaultSocketPath = ipc.DefaultSocketPath

// NewClient creates a new IPC client for the given socket path.
var NewClient = ipc.NewClient
