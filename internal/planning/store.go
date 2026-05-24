package planning

import (
	"context"
	"time"

	"github.com/igormaneschy/aurelia/internal/session"
)

// Status represents the planning state lifecycle.
type Status string

const (
	StatusActive       Status = "active"
	StatusAwaitingExec Status = "awaiting_exec"
	StatusCompleted    Status = "completed"
	StatusCancelled    Status = "cancelled"
)

// Phase represents the current planning phase.
type Phase string

const (
	PhaseSpecify Phase = "specify"
	PhaseDesign  Phase = "design"
	PhaseTasks   Phase = "tasks"
	PhaseReview  Phase = "review"
)

// Artifact tracks a file materialized during planning.
type Artifact struct {
	Path      string    // absolute or relative path
	Phase     Phase     // which phase created it
	Tool      string    // "Write", "Edit", "MultiEdit"
	InsideCWD bool      // resolved against cwd using filepath.Rel
	Confirmed bool      // confirmed by os.Stat after tool_result
	CreatedAt time.Time
}

// ProjectContext holds stat-only discovery results.
type ProjectContext struct {
	HasGit             bool
	HasClaudeMD        bool
	HasAgentsMD        bool
	HasReadme          bool
	Layouts            []string // e.g. "tlc", "rfc", "adr", "planning"
	NeedsLayoutChoice  bool
	Stacks             []string // e.g. "go", "node", "python", "rust"
	DiscoveredAt       time.Time
}

// State is the persistent planning state per SessionKey.
type State struct {
	Key              session.SessionKey
	Version          int       // optimistic locking
	Status           Status
	Phase            Phase
	CWD              string
	ProjectCtx       *ProjectContext
	Materialized     []Artifact
	LastHandoffError string
	HandoffStartedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Store persists planning state.
type Store interface {
	Get(ctx context.Context, key session.SessionKey) (*State, error)
	Save(ctx context.Context, state *State) error
	Delete(ctx context.Context, key session.SessionKey) error
	ListByUser(ctx context.Context, userID int64) ([]State, error)
	GC(ctx context.Context, maxAge time.Duration) error
	// Close releases the store's resources.
	Close() error
}

// OfferStore tracks planning intent offers with TTL.
type OfferStore interface {
	// RecordOffer records that an offer was made. Returns true if this is a new offer
	// or the previous offer has expired.
	RecordOffer(ctx context.Context, key session.SessionKey, intentHash string, ttl time.Duration) (bool, error)

	// HasRecentOffer checks if there's an unexpired offer for this key+intent.
	HasRecentOffer(ctx context.Context, key session.SessionKey, intentHash string) (bool, error)

	// GCOffers removes expired offers.
	GCOffers(ctx context.Context) error
}
