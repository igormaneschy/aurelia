package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/igormaneschy/aurelia/internal/runtime"
)

// lockPath returns the path to the instance lock file.
var lockPath = func() string {
	r, err := runtime.New()
	if err != nil {
		return filepath.Join(os.TempDir(), "aurelia-instance.lock")
	}
	return filepath.Join(r.Data(), "instance.lock")
}

// acquireLock attempts to acquire a singleton lock using a PID file with
// an advisory flock on the lock file descriptor. The lock is automatically
// released when the process exits (the OS releases the flock).
//
// If another instance is running, returns a descriptive error.
// If the lock is stale (process died without cleanup), removes it and retries.
func acquireLock() (release func(), err error) {
	path := lockPath()

	// 1. Check for stale lock via PID before attempting to lock.
	if existing, readErr := os.ReadFile(path); readErr == nil {
		existingPID, parseErr := strconv.Atoi(strings.TrimSpace(string(existing)))
		if parseErr == nil {
			proc, findErr := os.FindProcess(existingPID)
			if findErr == nil {
				if signalErr := proc.Signal(syscall.Signal(0)); signalErr == nil {
					return nil, fmt.Errorf("outra instância já está rodando (PID %d).\n"+
						"Use 'make deploy' para build + restart, ou:\n"+
						"  launchctl kickstart -k com.aurelia.agent   (restart limpo)\n"+
						"  launchctl stop com.aurelia.agent           (parar o daemon)",
						existingPID)
				}
			}
		}
		// Stale or corrupt — remove so we can acquire the lock below.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("warning: instance lock cleanup: %v", err)
		}
	}

	// 2. Open (or create) the lock file with RDWR so we can flock it.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("instance lock: open: %w", err)
	}

	// 3. Acquire exclusive flock. On Unix this is advisory — but since all
	//    future instances go through the same code path, it works reliably.
	//    Returns EWOULDBLOCK if another process holds the lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// If another process holds the lock AND wrote its PID first, the PID
		// check above should have caught it. If not (race), surface the error.
		return nil, fmt.Errorf("outra instância já está rodando (flock conflict)")
	}

	// 4. Truncate and write our PID.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock: truncate: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock: write PID: %w", err)
	}
	_ = f.Sync()

	// Release function closes the fd — this releases the flock automatically.
	return func() {
		_ = f.Close()
		_ = os.Remove(path)
	}, nil
}

// ensureSingleInstance is a convenience wrapper for main(). It acquires the
// instance lock and fatally exits if another instance is already running.
func ensureSingleInstance() (release func()) {
	release, err := acquireLock()
	if err != nil {
		log.Fatalf("❌ %v", err)
	}
	return release
}
