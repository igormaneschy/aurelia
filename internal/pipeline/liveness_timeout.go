package pipeline

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
)

// Liveness-aware idle watchdog (A2).
//
// The plain idle timeout canceled the run merely for silence. This watchdog
// separates three failure classes:
//  1. productive activity — events reset the idle window (unchanged);
//  2. alive but silent — the bridge answers a liveness probe, so the run is
//     NOT canceled: the user receives staged warnings (warning, then urgent)
//     and the model is steered, with a bounded grace budget before the
//     safety cancel;
//  3. dead or wedged — the liveness probe fails, so the run is canceled with
//     the process_death origin instead of pretending the provider was idle.
//
// The effective idle limit becomes idle + urgentLead + cancelLead (default
// 15–20min + 5min grace), still below the 30min max-execution safety net.
const (
	// livenessProbeTimeout bounds each bridge liveness probe.
	livenessProbeTimeout = 5 * time.Second

	// Escalation windows after the idle threshold fires while the bridge is
	// alive: warning → urgent → safety cancel.
	idleUrgentLead = 2 * time.Minute
	idleCancelLead = 3 * time.Minute

	// livenessNotifyTimeout bounds each user-facing notification (SendText +
	// steer). The Telegram transport itself is not context-aware; this
	// timeout keeps the notification worker from being pinned by a wedged
	// transport. The hook goroutine may linger until the transport call
	// returns — that residual belongs to the transport layer.
	livenessNotifyTimeout = 10 * time.Second

	timeoutOriginProcessDeath = "process_death"
)

// livenessPolicy drives the staged escalation with injectable windows so
// tests can run the whole state machine in milliseconds.
type livenessPolicy struct {
	idle       time.Duration // no-event threshold before probing
	urgentLead time.Duration // warning → urgent window
	cancelLead time.Duration // urgent → safety cancel window
	// notifyTimeout bounds each user-facing notification; 0 falls back to
	// livenessNotifyTimeout.
	notifyTimeout time.Duration
}

func productionLivenessPolicy(idle time.Duration) livenessPolicy {
	return livenessPolicy{idle: idle, urgentLead: idleUrgentLead, cancelLead: idleCancelLead, notifyTimeout: livenessNotifyTimeout}
}

// livenessHooks lets the watchdog probe and escalate without depending on the
// whole Service, keeping the wrapper unit-testable.
type livenessHooks struct {
	// probe verifies the bridge process is alive and responsive.
	probe func(ctx context.Context) error
	// warn is the surface-neutral escalation: progress state + runlog
	// telemetry (never a user-facing chat message).
	warn func(severity string, silent time.Duration)
	// notify sends a bounded user-facing warning and steers the model.
	// The wrapper calls it once per severity per escalation cycle (a
	// subsequent silence after activity resumes restarts the cycle).
	notify func(severity string, silent time.Duration)
}

// livenessIdleTimeoutWrapper wraps an events channel with the liveness-aware
// idle watchdog. Productive events reset the window, while telemetry passes
// through without masking silence. On idle expiry it probes liveness and
// escalates instead of canceling immediately. markTimeout receives the origin
// chosen by the watchdog (idle_bridge_timeout or process_death).
func livenessIdleTimeoutWrapper(ctx context.Context, ch <-chan bridge.Event, policy livenessPolicy, cancel context.CancelFunc, markTimeout func(origin string), hooks livenessHooks) <-chan bridge.Event {
	out := make(chan bridge.Event, cap(ch))
	notifyQueue := make(chan livenessNotification, 2)
	if hooks.notify != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in liveness notification worker: %v", r)
				}
			}()
			notifyTimeout := policy.notifyTimeout
			if notifyTimeout <= 0 {
				notifyTimeout = livenessNotifyTimeout
			}
			// The worker's lifetime is the wrapper's lifetime: it exits when
			// the watchdog closes notifyQueue (any exit path, including input
			// channel close without context cancellation).
			for notification := range notifyQueue {
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() {
						if r := recover(); r != nil {
							log.Printf("pipeline: panic in liveness notify hook: %v", r)
						}
					}()
					hooks.notify(notification.severity, notification.silent)
				}()
				// A wedged transport must not pin the worker after
				// cancellation: bound each notification call.
				timer := time.NewTimer(notifyTimeout)
				select {
				case <-done:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				case <-timer.C:
					log.Printf("pipeline: liveness notification (%s) timed out after %s", notification.severity, notifyTimeout)
				}
			}
		}()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in livenessIdleTimeoutWrapper: %v", r)
			}
		}()
		defer close(notifyQueue)
		defer close(out)

		lastEventAt := time.Now()
		// 0 = idle, 1 = warned, 2 = urgent (next expiry cancels).
		stage := 0
		window := policy.idle
		timer := time.NewTimer(window)
		defer timer.Stop()
		probeResults := make(chan livenessProbeResult, 1)
		probeGeneration := 0
		probing := false

		reset := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(window)
		}
		resetProductive := func() {
			lastEventAt = time.Now()
			stage = 0
			window = policy.idle
			probeGeneration++
			probing = false
			reset()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if livenessEventIsProductive(ev) {
					resetProductive()
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-timer.C:
				if probing {
					continue
				}
				probing = true
				generation := probeGeneration
				go func() {
					defer func() {
						if r := recover(); r != nil {
							select {
							case probeResults <- livenessProbeResult{generation: generation, err: fmt.Errorf("probe panic: %v", r)}:
							case <-ctx.Done():
							}
						}
					}()
					probeCtx, probeCancel := context.WithTimeout(ctx, livenessProbeTimeout)
					defer probeCancel()
					select {
					case probeResults <- livenessProbeResult{generation: generation, err: hooks.probe(probeCtx)}:
					case <-ctx.Done():
					}
				}()
			case result := <-probeResults:
				if result.generation != probeGeneration {
					continue
				}
				// A productive event may have arrived while the probe was in
				// flight but is still queued in ch: the select raced ahead of
				// the event case. Drain ready events before acting on the
				// probe result, so resumed activity resets the window instead
				// of being masked into a false escalation or cancellation.
				// The drain is bounded (at most one buffer's worth) and
				// observes ctx cancellation, so sustained telemetry can never
				// starve escalation or the cancel path.
				productiveArrived := false
				drained := 0
			drain:
				for drained < cap(ch) {
					select {
					case <-ctx.Done():
						return
					case ev, ok := <-ch:
						if !ok {
							return
						}
						if livenessEventIsProductive(ev) {
							productiveArrived = true
						}
						select {
						case out <- ev:
						case <-ctx.Done():
							return
						}
						drained++
					default:
						break drain
					}
				}
				if productiveArrived {
					// Resumed activity: reset the window and discard this
					// probe result (the generation bump makes it stale).
					resetProductive()
					continue
				}
				probing = false
				silent := time.Since(lastEventAt)
				if result.err != nil {
					// Dead or wedged bridge: cancel with the process-death
					// origin so the timeline explains the real failure.
					log.Printf("pipeline: idle watchdog liveness probe failed (%v) — canceling with origin %s", sanitizeForPersistence(result.err.Error(), maxRunlogErrorRunes), timeoutOriginProcessDeath)
					markTimeout(timeoutOriginProcessDeath)
					cancel()
					return
				}

				switch stage {
				case 0:
					if hooks.warn != nil {
						hooks.warn("warning", silent)
					}
					dispatchLivenessNotification(ctx, notifyQueue, livenessNotification{severity: "warning", silent: silent})
					stage = 1
					window = policy.urgentLead
				case 1:
					if hooks.warn != nil {
						hooks.warn("urgent", silent)
					}
					dispatchLivenessNotification(ctx, notifyQueue, livenessNotification{severity: "urgent", silent: silent})
					stage = 2
					window = policy.cancelLead
				case 2:
					log.Printf("pipeline: idle watchdog grace exhausted (silent=%s) — canceling with origin %s", silent.Round(time.Second), timeoutOriginIdleBridge)
					markTimeout(timeoutOriginIdleBridge)
					cancel()
					return
				}
				reset()
			}
		}
	}()

	return out
}

type livenessProbeResult struct {
	generation int
	err        error
}

type livenessNotification struct {
	severity string
	silent   time.Duration
}

func dispatchLivenessNotification(ctx context.Context, queue chan<- livenessNotification, notification livenessNotification) {
	select {
	case queue <- notification:
	case <-ctx.Done():
	default:
		log.Printf("pipeline: liveness notification queue full; continuing watchdog")
	}
}

// livenessEventIsProductive is deliberately narrower than the bridge event
// vocabulary. Lifecycle, heartbeat, compaction, stall, and steer telemetry
// describe activity but do not prove that the model produced progress.
// tool_use/tool_result only count when they carry a name or content: a
// wedged-but-alive bridge emitting empty tool events must not reset the
// window (bounded by the 30min hard execution cap in the pipeline).
func livenessEventIsProductive(ev bridge.Event) bool {
	switch ev.Type {
	case "result", "error":
		return true
	case "tool_use":
		return ev.Name != ""
	case "tool_result":
		return ev.ContentText() != "" || ev.ToolCallID != ""
	case "assistant":
		return ev.ContentText() != ""
	default:
		return false
	}
}

// stallHoldWindow is how long a reported stall state keeps the surface
// indicator against heartbeat Waiting re-beats. The heartbeat re-reports
// every interval*3 (30s); the bridge and watchdog stall sources re-report
// within 60–120s, so a 90s hold preserves the escalation between refreshes.
const stallHoldWindow = 90 * time.Second

// stallPriorityReporter decorates a ProgressReporter so the heartbeat
// Waiting re-beat cannot clobber a stall escalation (A2): while a stall was
// reported within stallHoldWindow, Waiting states are suppressed. Real
// productive activity (ProgressStateWorking — tool_use, delivered steer)
// still clears the stall line.
type stallPriorityReporter struct {
	mu        sync.Mutex
	inner     ProgressReporter
	stallSeen time.Time
}

func (s *stallPriorityReporter) ReportState(state ProgressState, detail string) {
	s.mu.Lock()
	switch state {
	case ProgressStateStallWarning, ProgressStateStallUrgent:
		s.stallSeen = time.Now()
	case ProgressStateWaiting:
		if time.Since(s.stallSeen) < stallHoldWindow {
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()
	s.inner.ReportState(state, detail)
}

func (s *stallPriorityReporter) ReportTool(toolName, detail string) {
	s.inner.ReportTool(toolName, detail)
}
func (s *stallPriorityReporter) ReportToolResult(summary string) { s.inner.ReportToolResult(summary) }
func (s *stallPriorityReporter) ReportText(text string)          { s.inner.ReportText(text) }
func (s *stallPriorityReporter) Delete()                         { s.inner.Delete() }

// wrapWithLivenessTimeout wires the liveness watchdog to the live Service:
// bridge ping probe, surface-neutral stall telemetry and bounded user
// warnings with steer.
func (s *Service) wrapWithLivenessTimeout(ctx context.Context, ch <-chan bridge.Event, chatID int64, threadID int, userID int64, idle time.Duration, cancel context.CancelFunc, timeoutTracker *runTimeoutTracker, progress ProgressReporter, runLogStarted bool, ownership runOwnership, steer func(msg string)) <-chan bridge.Event {
	return livenessIdleTimeoutWrapper(ctx, ch, productionLivenessPolicy(idle), cancel,
		func(origin string) { timeoutTracker.mark(origin) },
		livenessHooks{
			probe: func(pctx context.Context) error { return s.bridge.Ping(pctx) },
			warn: func(severity string, silent time.Duration) {
				if progress != nil {
					state := ProgressStateStallWarning
					if severity == "urgent" {
						state = ProgressStateStallUrgent
					}
					progress.ReportState(state, progressSilenceDetail(silent.Milliseconds()))
				}
				if s.runLog != nil && runLogStarted {
					s.countBridgeTelemetry(chatID, threadID, userID, "stall", ownership)
					s.recordPipelineEvent(chatID, threadID, userID, observability.NewWarnEvent("",
						observability.PhaseBridgeStall,
						fmt.Sprintf("event=idle_liveness severity=%s silent_ms=%d", severity, silent.Milliseconds())), ownership)
				}
			},
			notify: func(severity string, silent time.Duration) {
				if _, err := s.output.SendText(chatID, threadID, livenessWarningMessage(severity, silent)); err != nil {
					log.Printf("pipeline: SendText(liveness warning) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
				}
				if steer != nil {
					steer(livenessSteerPrompt(severity))
				}
			},
		})
}

func livenessWarningMessage(severity string, silent time.Duration) string {
	elapsed := silent.Round(time.Second)
	if severity == "urgent" {
		return fmt.Sprintf("⚠️ Sem resposta do processador há %s. Vou encerrar em ~%s se não houver atividade (checkpoint preservado — use /continuar para retomar).", elapsed, idleCancelLead)
	}
	return fmt.Sprintf("⏳ O processador está ativo mas não responde há %s. Vou continuar aguardando e encerro com checkpoint se não houver atividade em ~%s.", elapsed, idleUrgentLead+idleCancelLead)
}

func livenessSteerPrompt(severity string) string {
	if severity == "urgent" {
		return "Você está sem produzir resposta há vários minutos. Conclua imediatamente o que está fazendo e apresente um resumo parcial do que conseguiu até agora."
	}
	return "Continue: você está sem produzir resposta há vários minutos. Se terminou, apresente suas conclusões agora."
}
