package pipeline

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// visionAttempt carries per-turn vision state through event processing so a
// model that claims image support but refuses at runtime can be retried
// transparently with the configured vision fallback. It is Plumbed through
// ProcessBridgeEvents/handleResultEvent as an explicit pointer (nil = no
// vision handling, e.g. text-only turns and the TUI path).
type visionAttempt struct {
	images   []bridge.ImageAttachment
	canRetry bool
	retried  bool
	// refusalText stashes the suppressed refusal so a failed retry can still
	// deliver an honest answer instead of dropping the turn silently.
	refusalText string
}

// newVisionAttempt decides whether a turn qualifies for reactive vision
// retry: images were actually sent AND a vision fallback is configured AND
// it differs from the current model (otherwise a retry would loop onto the
// same model). Returns nil when there is nothing to retry, keeping the
// hot path allocation-free for text-only turns.
func newVisionAttempt(req bridge.Request, cfg *config.AppConfig) *visionAttempt {
	if len(req.Options.Images) == 0 || cfg == nil {
		return nil
	}
	fbModel, fbProvider := cfg.VisionFallback()
	if fbModel == "" {
		return nil
	}
	if req.Options.Provider == fbProvider && req.Options.Model == fbModel {
		return nil
	}
	return &visionAttempt{images: req.Options.Images, canRetry: true}
}

// visionRefusalStrong matches standalone refusal admissions.
var visionRefusalStrong = []string{
	"imagem veio vazia",
	"sem conteudo visual",
	"nao consigo visualizar",
	"no image was provided",
	"no image attached",
	"no image received",
	"no image was attached",
	"image came through empty",
}

// visionRefusalWeak matches refusal fragments that only count when the
// reply also mentions image-like content (avoids matching unrelated
// "não consigo ver" remarks in long analyses).
var visionRefusalWeak = []string{
	"nao consigo ver",
	"nao posso ver",
	"nao consigo processar",
	"nao tenho capacidade",
	"nao sou capaz de ver",
	"cant see",
	"can't see",
	"cannot see",
	"unable to see",
	"unable to view",
	"cant view",
	"can't view",
	"cannot view",
	"not able to see",
	"dont have the ability",
	"don't have the ability",
	"apenas texto",
	"text-only",
	"text only",
	"nao esta chegando",
	"reenviar",
	"reenvie",
}

// visionImageTokens confirms the reply is about visual content.
var visionImageTokens = []string{
	"imagem", "foto", "visual", "figura", "picture", "photo", "image",
}

// maxVisionRefusalRunes bounds heuristic matching to short replies. Genuine
// image analyses are long; refusals are a sentence or two. This keeps a
// stray "não consigo ver" inside a long answer from triggering a retry.
const maxVisionRefusalRunes = 2000

// isVisionRefusal reports whether an assistant reply looks like a vision
// capability refusal ("a imagem veio vazia", "I can't see images", ...).
// Input is normalized (lowercase, accents stripped) before matching.
func isVisionRefusal(text string) bool {
	n := normalizeConcurrentText(text)
	if n == "" {
		return false
	}
	if len([]rune(n)) > maxVisionRefusalRunes {
		return false
	}
	for _, p := range visionRefusalStrong {
		if strings.Contains(n, p) {
			return true
		}
	}
	hasImageToken := false
	for _, tok := range visionImageTokens {
		if strings.Contains(n, tok) {
			hasImageToken = true
			break
		}
	}
	if !hasImageToken {
		return false
	}
	for _, p := range visionRefusalWeak {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// retryVisionFallback re-issues the turn with the configured vision fallback
// model after the primary model refused image input. It runs inside the
// same run/slot/ownership: the runlog stays open and the retry lands in the
// same run, so the user only ever sees the fallback answer. Single-shot by
// construction (the retry passes a spent attempt), so no loop is possible.
func (s *Service) retryVisionFallback(
	ctx context.Context,
	cancel context.CancelFunc,
	chatID int64,
	threadID int,
	messageID int,
	req bridge.Request,
	userText string,
	userID int64,
	isPrivateChat bool,
	progress ProgressReporter,
	toolUseSignal chan<- struct{},
	toolTracker *toolCallTracker,
	loopDetect *loopDetector,
	timeoutTracker *runTimeoutTracker,
	runLogStarted bool,
	ownership runOwnership,
	steer func(string),
	vision *visionAttempt,
) Outcome {
	fbModel, fbProvider := s.config.VisionFallback()
	fbReq := req
	fbReq.Options.Model = fbModel
	if fbProvider != "" {
		fbReq.Options.Provider = fbProvider
	}
	fbReq.RequestID = fmt.Sprintf("run-%d", time.Now().UnixNano())

	log.Printf("vision: refusal detected, transparent retry with fallback chat=%d model=%s", chatID, fbModel)
	if runLogStarted {
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewWarnEvent("",
			observability.PhaseRetryStarted,
			fmt.Sprintf("origin=vision_refusal fallback_model=%s", fbModel)), ownership)
	}

	ch, _, err := s.executeQuery(ctx, fbReq, func(msg string) {
		if _, sendErr := s.output.SendText(chatID, threadID, msg); sendErr != nil {
			log.Printf("pipeline: SendText(vision fallback status) failed for chat=%d: %s", chatID, sanitizeForPersistence(sendErr.Error(), maxRunlogErrorRunes))
		}
	})
	if err != nil {
		log.Printf("vision: fallback query failed chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		return s.deliverStashedRefusal(chatID, threadID, messageID, userID, runLogStarted, ownership, vision)
	}

	ch = s.wrapWithLivenessTimeout(ctx, ch, chatID, threadID, userID, s.getIdleTimeout(), cancel, timeoutTracker, progress, runLogStarted, ownership, steer)
	spent := &visionAttempt{images: vision.images, canRetry: false, retried: true}
	return s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID, isPrivateChat, toolTracker, loopDetect, spent, ownership)
}

// deliverStashedRefusal completes the run with the original refusal text
// when the fallback query itself fails. Honest degradation: the user sees
// what the primary model said instead of silence.
func (s *Service) deliverStashedRefusal(chatID int64, threadID int, messageID int, userID int64, runLogStarted bool, ownership runOwnership, vision *visionAttempt) Outcome {
	text := strings.TrimSpace(vision.refusalText)
	if text == "" {
		text = bridgeEmptyResultMessage
	}
	if runLogStarted {
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
			observability.PhaseRunFailed, "vision_fallback_failed"), ownership)
		s.patchContinuityFailure(chatID, threadID, "failed", "vision_fallback_failed", userID, false, ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "vision_fallback_failed", ownership)
	}
	if _, err := s.output.SendReply(chatID, threadID, text); err != nil {
		log.Printf("pipeline: SendReply(stashed refusal) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
	}
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}
