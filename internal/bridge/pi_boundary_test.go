package bridge

import (
	"strings"
	"testing"
)

// ── PI Boundary Contract Tests ──────────────────────────────────────────────
//
// These source-contract tests guard against accidental drift of critical
// PI SDK integration choices. Each test asserts a specific architecture
// boundary decision. If a test fails, review the corresponding decision
// carefully — changing any of these may break the PI/Aurelia boundary
// contract without the companion Go code being updated.
//
// See .specs/project/ROADMAP.md and docs/pi-sdk-api-validation.md
// for the full architecture decisions.

// activeCodeLines returns lines from the embedded PI Bridge TypeScript source
// that are non-empty and not solely a // line comment. Inline // comments
// are stripped so trailing explanations are ignored.
//
// NOTE: Block comments (/* */) are NOT stripped because the bridge source
// does not use them and the /* pattern appears inside string literals
// (e.g., glob patterns like ".ssh/*"). A full-comment-aware lexer would
// be needed to distinguish those cases; the current approach is sufficient
// because all comments in the bridge source are //-style.
func activeCodeLines() []string {
	src := string(EmbeddedBridgeTS)
	lines := strings.Split(src, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Strip // line comments (including inline trailing comments).
		if idx := strings.Index(trimmed, "//"); idx != -1 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

// activeCodeContains reports whether any processed line contains substr.
func activeCodeContains(substr string) bool {
	for _, line := range activeCodeLines() {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// activeCodeWindowContains reports whether target appears within `window`
// lines (in the processed active-code view) after a line containing `marker`.
// This enables contextual assertions like "session_file appears near
// event: 'system'".
func activeCodeWindowContains(marker, target string, window int) bool {
	lines := activeCodeLines()
	for i, line := range lines {
		if strings.Contains(line, marker) {
			end := i + 1 + window
			if end > len(lines) {
				end = len(lines)
			}
			for _, after := range lines[i+1 : end] {
				if strings.Contains(after, target) {
					return true
				}
			}
		}
	}
	return false
}

// TestBridgeDefaultResourceLoader_noContextFiles ensures PI discovers
// project context files (CLAUDE.md, AGENTS.md) by default.
//
// Architecture decision: Aurelia lets PI own context file discovery.
// Setting noContextFiles: true would prevent PI from loading project
// context files, breaking the "PI owns engine concerns" contract.
func TestBridgeDefaultResourceLoader_noContextFiles(t *testing.T) {
	if !activeCodeContains("noContextFiles: false") {
		t.Fatal("PI boundary violation: DefaultResourceLoader must set noContextFiles: false " +
			"in active code (not just comments). " +
			"This lets PI discover CLAUDE.md/AGENTS.md. " +
			"If this was intentionally changed, update the architecture decision in " +
			"docs/pi-sdk-api-validation.md and the Go companion code.")
	}
}

// TestBridgeSettingsManager_inMemoryCompaction ensures compaction is enabled for
// in-memory settings (no_user_settings mode), preventing unbounded
// session growth in memory-only mode.
//
// Architecture decision: no-user-settings/internal runs use
// SettingsManager.inMemory with compaction enabled by default.
func TestBridgeSettingsManager_inMemoryCompaction(t *testing.T) {
	if !activeCodeContains("compaction: { enabled: true }") {
		t.Fatal("PI boundary violation: SettingsManager.inMemory() must enable compaction " +
			"in active code (not just comments). " +
			"Without compaction, sessions may grow unbounded in memory-only mode.")
	}
}

// TestBridgeSecurityHook_usesBeforeToolCall ensures security preflight wraps
// the PI SDK hook (session.agent.beforeToolCall) rather than the non-existent
// session.on("tool_call"). This is the correct PI SDK API for tool preflight.
//
// Architecture decision: Security enforcement uses the PI SDK's
// beforeToolCall hook, not the legacy Claude SDK session event pattern.
func TestBridgeSecurityHook_usesBeforeToolCall(t *testing.T) {
	// 1. Assignment: the hook is actively wrapped by assigning a new function.
	if !activeCodeContains("export function installSecurityHook(") ||
		!activeCodeContains("agent.beforeToolCall = async") {
		t.Fatal("PI boundary violation: Security preflight must wrap " +
			"session.agent.beforeToolCall (the PI SDK tool preflight hook) in active code, " +
			"not merely mention it in comments. " +
			"Session.on(\"tool_call\") does not exist in @earendil-works/pi-coding-agent " +
			"and must never be used.")
	}

	// 2. Save: the original hook is captured before wrapping.
	if !activeCodeContains("const origBeforeToolCall = agent.beforeToolCall") {
		t.Fatal("PI boundary violation: Security preflight must save the original " +
			"session.agent.beforeToolCall into const origBeforeToolCall before wrapping. " +
			"Without this, the extension-runner hook cannot be restored on teardown.")
	}

	// 3. Restore: the original hook is restored when the security wrapper is torn down.
	if !activeCodeContains("agent.beforeToolCall = origBeforeToolCall") {
		t.Fatal("PI boundary violation: Security preflight must restore the original " +
			"session.agent.beforeToolCall from origBeforeToolCall on teardown. " +
			"Without this, PI extensions remain bypassed after security is removed.")
	}
}

// TestBridgeSessionOnToolCall_absent ensures the deprecated session.on("tool_call")
// pattern is NOT used in active code. The PI SDK exposes session.agent.beforeToolCall
// for tool preflight; session.on("tool_call") is a legacy Claude SDK pattern
// that does not exist in the PI SDK.
func TestBridgeSessionOnToolCall_absent(t *testing.T) {
	for _, line := range activeCodeLines() {
		if strings.Contains(line, `session.on("tool_call")`) ||
			strings.Contains(line, `session.on('tool_call')`) ||
			strings.Contains(line, "session.on(`tool_call`)") {
			t.Fatalf("PI boundary violation: session.on(\"tool_call\") appears in active code at:\n%s\n"+
				"The PI SDK exposes session.agent.beforeToolCall for tool preflight. "+
				"Remove any session.on(\"tool_call\") usage immediately.", line)
		}
	}
}

func TestBridgeHealthTimer_visibleToFinally(t *testing.T) {
	if !activeCodeContains("let healthTimer: ReturnType<typeof setInterval> | undefined") {
		t.Fatal("bridge healthTimer must be declared before query setup so finally can clear it after early errors")
	}
	if !activeCodeContains("healthTimer = setInterval(() => {") {
		t.Fatal("bridge healthTimer must assign the outer variable, not shadow it inside try")
	}
	if activeCodeContains("const healthTimer = setInterval") {
		t.Fatal("bridge healthTimer must not be block-scoped with const; finally must be able to clear it")
	}
	if !activeCodeContains("if (healthTimer) clearInterval(healthTimer)") {
		t.Fatal("bridge finally must guard and clear healthTimer")
	}
}

func TestBridgeSessionHistory_normalizesNumericTimestamps(t *testing.T) {
	if !activeCodeContains("function sessionMessageTimestampISO") {
		t.Fatal("bridge session history must normalize PI message timestamps before returning TUI history")
	}
	if !activeCodeContains(`typeof raw === "number" && Number.isFinite(raw)`) {
		t.Fatal("bridge session history must support PI numeric Unix-ms timestamps")
	}
	if activeCodeContains("history.push({ sender, text, timestamp: ts })") {
		t.Fatal("bridge session history must not stamp restored messages with current time")
	}
}

// TestBridgeSessionFile_emitted ensures the bridge emits session_file in
// both system and result events so Aurelia can track session persistence.
// This is the key mechanism for Aurelia's session management layer.
//
// Architecture decision: Session tracking runs through bridge-emitted
// session_file references, not direct file-system access.
func TestBridgeSessionFile_emitted(t *testing.T) {
	const target = "session_file: liveSession.sessionFile"

	// Must appear within the system event emission block (event: "system").
	if !activeCodeWindowContains(`event: "system"`, target, 10) {
		t.Fatalf("PI boundary violation: session_file must appear within a few lines "+
			"after event: \"system\" in active code. "+
			"Expected %q after the system event emission. "+
			"This field is critical for session persistence tracking.", target)
	}

	// Must also appear within the query result emission block (event: "result").
	if !activeCodeWindowContains(`event: "result"`, target, 10) {
		t.Fatalf("PI boundary violation: session_file must appear within a few lines "+
			"after event: \"result\" in active code. "+
			"Expected %q after the result event emission. "+
			"This field is critical for session persistence tracking.", target)
	}
}

// TestBridgeListModels_resolveModelFilter verifies data-flow invariants in the
// list-models handler: models from ModelRuntime.getAvailable() must be filtered
// through the same ModelRuntime-backed resolveModel path before being exposed as
// selectable.
//
// The assertions enforce:
//  1. summary is NOT derived directly from available (which would skip filtering).
//  2. resolveModel is called for each model inside the filter callback.
//  3. A redacted diagnostic is logged when unresolvable models are excluded.
//
// Without this filter, models returned by getAvailable() that registry.find()
// cannot resolve (due to provider/model name mapping differences) would be
// shown as selectable but fail at query time with "model not found in PI registry".
//
// Regression test for the fix that restored model selection reliability
// after the PI SDK integration (see bridge/index.ts list-models handler).
func TestBridgeListModels_resolveModelFilter(t *testing.T) {
	// INVARIANT 1: Summary must derive from filtered, NOT from available directly.
	// This catches a regression where getAvailable() results are mapped to output
	// while resolveModel filtering is only logged but not applied.
	if activeCodeContains("const summary = available.map") {
		t.Fatal("PI boundary violation: list-models output must derive from " +
			"the filtered result, not directly from 'available'. " +
			"Using 'const summary = available.map(...)' skips the resolveModel " +
			"filter and can expose models that fail at query time " +
			"with 'model not found in PI registry'.")
	}

	// INVARIANT 2: The filter callback must call resolveModel to validate each model.
	// The pattern 'resolveModel(modelRegistry, m.provider, m.id' is specific to the
	// filter context (the createPiSession call uses opts?.provider, opts?.model).
	if !activeCodeContains("resolveModel(modelRuntime, m.provider, m.id") {
		t.Fatal("PI boundary violation: list-models filter must call resolveModel " +
			"for each model to verify it can be found at query time. " +
			"Expected 'resolveModel(modelRuntime, m.provider, m.id)' " +
			"inside the filter callback.")
	}

	// INVARIANT 3: Redacted diagnostic logged when models are excluded.
	if !activeCodeContains("list-models: excluding unresolvable model") {
		t.Fatal("PI boundary violation: list-models handler must log " +
			"a redacted diagnostic when resolveModel filtering excludes a model. " +
			"Expected 'redactedLog(`list-models: excluding unresolvable model ...`)'.")
	}
}

func TestBridgeModelRuntimeBoundary(t *testing.T) {
	if !activeCodeContains("ModelRuntime.create({") {
		t.Fatal("PI boundary violation: bridge must create ModelRuntime explicitly")
	}
	if !activeCodeContains("modelsStorePath: join(agentDir, \"models-store.json\")") {
		t.Fatal("PI boundary violation: models-store.json must remain in Aurelia's isolated agent directory")
	}
	if !activeCodeContains("modelRuntime.getModel(mappedProvider, mappedModel)") ||
		!activeCodeContains("modelRuntime.getModels().find((m) => m.id === mappedModel)") {
		t.Fatal("PI boundary violation: model resolution must use qualified getModel then exact-ID getModels fallback")
	}
	if activeCodeContains("AuthStorage.create(") || activeCodeContains("ModelRegistry.create(") {
		t.Fatal("PI boundary violation: bridge must not use removed AuthStorage or ModelRegistry construction")
	}
	if !activeCodeContains("await modelRuntime.refresh({") || !activeCodeContains("allowNetwork: true") || !activeCodeContains("force: true") {
		t.Fatal("PI boundary violation: catalog network refresh must be explicit with allowNetwork and force")
	}
}
