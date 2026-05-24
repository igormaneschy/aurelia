package pipeline

import (
	"context"
	"time"

	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

// offerTTL is the duration during which repeated planning intent is throttled.
const offerTTL = 5 * time.Minute

// planningOfferIntentHash is used as the intent hash for OfferStore,
// grouping all planning intent offers together for throttling.
const planningOfferIntentHash = "planning_intent_offer"

// planningOfferMessage is the user-facing offer text.
const planningOfferMessage = `Parece que você quer planejar algo. Quer entrar no Modo Plano? Use /plan para começar.`

// maybeOfferPlanning checks if we should offer Plan Mode.
// Returns (offerSent bool, message string).
// If offerSent is true, the caller should send the message and skip bridge call.
func (s *Service) maybeOfferPlanning(ctx context.Context, text string, key session.SessionKey) (bool, string) {
	// 1. Active planning state — no offer needed
	localKey := sessionKey(key.ChatID, key.ThreadID, key.UserID)
	if _, ok := s.planningStates.Load(localKey); ok {
		return false, ""
	}

	// 2. No planning intent — no offer
	if !looksLikePlanningIntent(text) {
		return false, ""
	}

	// 3. Throttle via OfferStore (type-assert from planningStore)
	offerStore, ok := s.planningStore.(planning.OfferStore)
	if !ok {
		return false, ""
	}

	recent, err := offerStore.HasRecentOffer(ctx, key, planningOfferIntentHash)
	if err != nil || recent {
		return false, ""
	}

	accepted, err := offerStore.RecordOffer(ctx, key, planningOfferIntentHash, offerTTL)
	if err != nil || !accepted {
		return false, ""
	}

	return true, planningOfferMessage
}
