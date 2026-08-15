package pipeline

import (
	"context"
	"fmt"
	"log"
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

	timeoutOriginProcessDeath = "process_death"
)

// livenessPolicy drives the staged escalation with injectable windows so
// tests can run the whole state machine in milliseconds.
type livenessPolicy struct {
	idle       time.Duration // no-event threshold before probing
	urgentLead time.Duration // warning → urgent window
	cancelLead time.Duration // urgent → safety cancel window
}

func productionLivenessPolicy(idle time.Duration) livenessPolicy {
	return livenessPolicy{idle: idle, urgentLead: idleUrgentLead, cancelLead: idleCancelLead}
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
	// The wrapper calls it once per severity by construction.
	notify func(severity string, silent time.Duration)
}

// livenessIdleTimeoutWrapper wraps an events channel with the liveness-aware
// idle watchdog. It behaves like the old idle timeout (events reset the
// window, ctx.Done exits) but on idle expiry it probes liveness and escalates
// instead of canceling immediately. markTimeout receives the origin chosen by
// the watchdog (idle_bridge_timeout or process_death).
func livenessIdleTimeoutWrapper(ctx context.Context, ch <-chan bridge.Event, policy livenessPolicy, cancel context.CancelFunc, markTimeout func(origin string), hooks livenessHooks) <-chan bridge.Event {
	out := make(chan bridge.Event, cap(ch))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in livenessIdleTimeoutWrapper: %v", r)
			}
		}()
		defer close(out)

		lastEventAt := time.Now()
		// 0 = idle, 1 = warned, 2 = urgent (next expiry cancels).
		stage := 0
		window := policy.idle
		timer := time.NewTimer(window)
		defer timer.Stop()

		reset := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(window)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				lastEventAt = time.Now()
				stage = 0
				window = policy.idle
				reset()
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-timer.C:
				silent := time.Since(lastEventAt)
				probeCtx, probeCancel := context.WithTimeout(context.Background(), livenessProbeTimeout)
				probeErr := hooks.probe(probeCtx)
				probeCancel()

				if probeErr != nil {
					// Dead or wedged bridge: cancel with the process-death
					// origin so the timeline explains the real failure.
					log.Printf("pipeline: idle watchdog liveness probe failed (%v) — canceling with origin %s", sanitizeForPersistence(probeErr.Error(), maxRunlogErrorRunes), timeoutOriginProcessDeath)
					markTimeout(timeoutOriginProcessDeath)
					cancel()
					return
				}

				switch stage {
				case 0:
					if hooks.warn != nil {
						hooks.warn("warning", silent)
					}
					if hooks.notify != nil {
						hooks.notify("warning", silent)
					}
					stage = 1
					window = policy.urgentLead
				case 1:
					if hooks.warn != nil {
						hooks.warn("urgent", silent)
					}
					if hooks.notify != nil {
						hooks.notify("urgent", silent)
					}
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
