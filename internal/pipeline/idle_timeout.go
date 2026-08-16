package pipeline

// The liveness-aware idle watchdog lives in liveness_timeout.go. The legacy
// idleTimeoutWrapper (cancel on silence, no probe, no staged warnings) was
// removed when the liveness watchdog replaced it at both call sites.
