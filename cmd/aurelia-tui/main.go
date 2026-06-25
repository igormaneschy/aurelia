// Binary aurelia-tui is a terminal UI client for the Aurelia daemon.
// It connects to the daemon over a Unix socket and provides a chat-like
// interface for sending messages and commands, with streaming responses
// and markdown rendering.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/tui"
)

func main() {
	themeFlag := flag.String("theme", "auto", "TUI theme: auto, light, or dark")
	noAnimations := flag.Bool("no-animations", false, "Disable TUI animations")
	noMouse := flag.Bool("no-mouse", false, "Disable mouse interaction (Ctrl+O has no effect)")
	flag.Parse()

	theme := tui.ParseTheme(*themeFlag)
	switch theme {
	case tui.ThemeAuto, tui.ThemeLight, tui.ThemeDark:
		// valid
	default:
		fmt.Fprintf(os.Stderr, "Error: --theme must be auto, light, or dark, got %q\n", *themeFlag)
		os.Exit(1)
	}

	// Determine socket path.
	socketPath, err := tui.DefaultSocketPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine IPC socket path: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure the Aurelia daemon is running.\n")
		os.Exit(1)
	}

	// Create and start the Bubble Tea program. The UI itself owns daemon
	// reachability so startup stays visual even when the socket is missing.
	// Mouse capture is opt-in via Ctrl+O so native terminal text selection works
	// by default.
	m := tui.NewModelWithOptions(socketPath, theme, tui.ModelOptions{
		NoAnimations: *noAnimations,
		NoMouse:      *noMouse,
	})
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if m, ok := finalModel.(tui.Model); ok {
		if err := m.SaveInputHistory(); err != nil {
			log.Printf("warning: failed to save input history: %v", err)
		}
	}
	if err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}