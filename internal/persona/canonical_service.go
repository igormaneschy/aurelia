package persona

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igormaneschy/aurelia/internal/users"
)

// CanonicalIdentityService loads persona files and builds a system prompt.
type CanonicalIdentityService struct {
	identityPath        string
	soulPath            string
	userPath            string
	ownerPlaybookPath   string
	lessonsLearnedPath  string
	projectPlaybookPath string
}

// UserPromptResolver provides paths for per-user persona files.
type UserPromptResolver interface {
	UserMdPath(userID int64) string
	UserModePath(userID int64, mode string) string
}

// NewCanonicalIdentityService creates a persona loader.
func NewCanonicalIdentityService(
	identityPath, soulPath, userPath string,
	ownerPlaybookPath, lessonsLearnedPath string,
	projectPlaybookPath string,
) *CanonicalIdentityService {
	return &CanonicalIdentityService{
		identityPath:        identityPath,
		soulPath:            soulPath,
		userPath:            userPath,
		ownerPlaybookPath:   ownerPlaybookPath,
		lessonsLearnedPath:  lessonsLearnedPath,
		projectPlaybookPath: projectPlaybookPath,
	}
}

// PromptOptions controls optional prompt injections.
type PromptOptions struct {
	IsOwner bool
}

// BuildPrompt loads persona files and returns the assembled system prompt.
// Deprecated: Use BuildPromptForUser for per-user persona isolation.
func (s *CanonicalIdentityService) BuildPrompt() (string, error) {
	p, err := LoadPersona(s.identityPath, s.soulPath, s.userPath)
	if err != nil {
		return "", err
	}

	prompt := p.SystemPrompt

	ownerBlock := s.buildOwnerDocsBlock()
	if ownerBlock != "" {
		prompt = prompt + "\n\n" + ownerBlock
	}

	projectBlock := s.buildProjectBlock()
	if projectBlock != "" {
		prompt = prompt + "\n\n" + projectBlock
	}

	return prompt, nil
}

// BuildPromptForUser loads IDENTITY + SOUL (global) and USER.md (per-user),
// injecting owner docs only when isOwner is true. When activeMode is a
// non-empty, normalized mode, appends the mode overlay file last.
func (s *CanonicalIdentityService) BuildPromptForUser(userID int64, resolver UserPromptResolver, isOwner bool, activeMode string) (string, error) {
	identityBytes, err := readPersonaFile(s.identityPath, "identity")
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}
	soulBytes, err := readPersonaFile(s.soulPath, "soul")
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}

	// Load per-user USER.md, falling back to a minimal stub if missing.
	userMdPath := resolver.UserMdPath(userID)
	userBytes, err := os.ReadFile(userMdPath)
	if err != nil {
		log.Printf("persona: per-user USER.md not found at %q for user %d, using stub", userMdPath, userID)
		userBytes = []byte(fmt.Sprintf("---\nuser_id: %d\nname: User\nlanguage: pt\n---\n\n# User\n\nUser id: %d\n", userID, userID))
	}

	// Fallback: if per-user USER.md is a stub (just "User id: <N>" with no bio),
	// append the global USER.md content for richer persona context.
	if len(userBytes) > 0 {
		body := extractUserMdBody(userBytes)
		if isStubUserMd(body) {
			globalUserBytes, gErr := os.ReadFile(s.userPath)
			if gErr == nil && len(globalUserBytes) > 0 {
				globalBody := extractUserMdBody(globalUserBytes)
				if globalBody != "" {
					// Append as legacy preferences note
					combined := string(userBytes)
					if !strings.HasSuffix(combined, "\n") {
						combined += "\n"
					}
					combined += "\n# Legacy Preferences\n\n" + globalBody + "\n"
					userBytes = []byte(combined)
				}
			}
		}
	}

	config, identityBody, err := parseIdentityFrontmatter(identityBytes)
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}

	promptBody := buildPromptBody(identityBody, soulBytes, userBytes)

	canonicalIdentity := CanonicalIdentity{
		AgentName: canonicalValue(config.Name),
		AgentRole: canonicalValue(config.Role),
		UserName:  canonicalUserName(userBytes),
	}

	prompt := buildSystemPrompt(canonicalIdentity, promptBody)

	if isOwner {
		ownerBlock := s.buildOwnerDocsBlock()
		if ownerBlock != "" {
			prompt = prompt + "\n\n" + ownerBlock
		}
	}

	projectBlock := s.buildProjectBlock()
	if projectBlock != "" {
		prompt = prompt + "\n\n" + projectBlock
	}

	// Active mode overlay — appended LAST.
	if modeBlock := s.buildModeBlock(userID, resolver, activeMode); modeBlock != "" {
		prompt = prompt + "\n\n" + modeBlock
	}

	return prompt, nil
}

// buildModeBlock reads the mode overlay file and returns a formatted prompt
// block. Returns "" when mode is empty, invalid, or the overlay file is missing.
func (s *CanonicalIdentityService) buildModeBlock(userID int64, resolver UserPromptResolver, mode string) string {
	mode, err := users.NormalizeMode(mode)
	if err != nil || mode == "" {
		return ""
	}
	content, err := readOptionalFile(resolver.UserModePath(userID, mode))
	if err != nil {
		log.Printf("persona: failed to read mode overlay user=%d mode=%s: %v", userID, mode, err)
		return ""
	}
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return "## ACTIVE MODE: " + strings.ToUpper(mode) + "\n\n" + strings.TrimSpace(content)
}

// extractUserMdBody returns the body of a USER.md file after the YAML frontmatter.
func extractUserMdBody(data []byte) string {
	content := string(data)
	// Remove frontmatter between --- markers
	if strings.HasPrefix(content, "---") {
		idx := strings.Index(content[3:], "---")
		if idx >= 0 {
			content = content[idx+6:] // skip closing ---
		}
	}
	return strings.TrimSpace(content)
}

// isStubUserMd returns true if the body is just an auto-generated "User id: <number>" stub.
// Stub formats (both without meaningful bio content):
//   - "# User Profile\n\nUser id: <number>"  (from writeUserMd with empty bio)
//   - "# User\n\nUser id: <number>"           (from BuildPromptForUser fallback)
func isStubUserMd(body string) bool {
	if body == "" {
		return true
	}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "# User Profile") || strings.HasPrefix(trimmed, "# User") {
		// Check if after the header there's only "User id: <N>"
		lines := strings.SplitN(trimmed, "\n", 3)
		if len(lines) >= 2 && strings.Contains(lines[len(lines)-1], "User id") {
			return true
		}
	}
	return false
}
