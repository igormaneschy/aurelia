// Binary aurelia-tui is a terminal UI client for the Aurelia daemon.
// It connects to the daemon over a Unix socket and provides a chat-like
// interface for sending messages and commands, with streaming responses
// and markdown rendering.
package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/igormaneschy/aurelia/internal/tui"
)

func main() {
	// Determine socket path.
	socketPath, err := tui.DefaultSocketPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine IPC socket path: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure the Aurelia daemon is running.\n")
		os.Exit(1)
	}

	// Create and start the Bubble Tea program. The UI itself owns daemon
	// reachability so startup stays visual even when the socket is missing.
	m := tui.NewModel(socketPath)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}
