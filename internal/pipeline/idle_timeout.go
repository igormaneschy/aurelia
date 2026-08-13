package pipeline

import (
	"context"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// idleTimeoutWrapper wraps an events channel with an idle timeout.
// If no event arrives within idleDuration, markTimeout and cancel are called.
// When the input channel closes or ctx is done, the wrapper shuts down cleanly.
func idleTimeoutWrapper(ctx context.Context, ch <-chan bridge.Event, idleDuration time.Duration, cancel context.CancelFunc, markTimeout func()) <-chan bridge.Event {
	out := make(chan bridge.Event, cap(ch))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in idleTimeoutWrapper: %v", r)
			}
		}()
		defer close(out)

		timer := time.NewTimer(idleDuration)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				// Reset idle timer on each event
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleDuration)

				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-timer.C:
				if markTimeout != nil {
					markTimeout()
				}
				log.Printf("pipeline: idle timeout (%s) — cancelling context", idleDuration)
				cancel()
				return
			}
		}
	}()

	return out
}
