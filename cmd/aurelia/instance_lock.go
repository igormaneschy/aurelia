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

	// 1. Open (or create) the lock file with RDWR so we can flock it.
	// O_NOFOLLOW prevents a symlink at the lock path from making startup
	// truncate or overwrite an unrelated file.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("instance lock: open: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock: stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock: %s is not a regular file", path)
	}

	// 2. The flock is authoritative. A PID file can outlive its process (crash)
	//    or be meaningless inside a PID namespace (ai-jail maps the daemon to a
	//    low PID), so it is only diagnostic after the flock fails.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		existing := ""
		if data, readErr := os.ReadFile(path); readErr == nil {
			existing = strings.TrimSpace(string(data))
		}
		_ = f.Close()
		if existing != "" {
			return nil, fmt.Errorf("outra instância já está rodando (PID %s); pare o processo ou reinicie o serviço", existing)
		}
		return nil, fmt.Errorf("outra instância já está rodando (flock conflict)")
	}

	// 3. Truncate and write our PID for diagnostics.
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
	// The lock file stays in place: unlinking after releasing the flock lets a
	// second process acquire the old inode while a third creates a new one.
	return func() {
		_ = f.Close()
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
