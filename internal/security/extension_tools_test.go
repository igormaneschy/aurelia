package security

import (
	"reflect"
	"testing"
)

// containsTool is declared in profiles_test.go (same package).
// The observe profile grants no tools at all; extension tools must not widen it.
func TestProfileExtensionTools_ObserveGrantsNothing(t *testing.T) {
	if got := ProfileExtensionTools(ProfileObserve); got != nil {
		t.Fatalf("ProfileExtensionTools(observe) = %v, want nil", got)
	}
	if got := GrantExtensionTools(ProfileObserve, nil, nil); got != nil {
		t.Fatalf("GrantExtensionTools(observe, nil) = %v, want nil", got)
	}
}

// Read-only contexts read the wiki but never write pages, and never get the
// MCP scripting tool (which can call arbitrary MCP tools).
func TestProfileExtensionTools_ReadOnlyIsReadOnly(t *testing.T) {
	tools := ProfileExtensionTools(ProfileReadOnly)
	for _, want := range []string{"memory_query", "memory_read_page", "memory_explore", "memory_read_session_observations"} {
		if !containsTool(tools, want) {
			t.Errorf("read_only should grant %q, got %v", want, tools)
		}
	}
	for _, banned := range []string{"memory_write_page", "memory_delete_page", "mcpScript", "memory_consolidate"} {
		if containsTool(tools, banned) {
			t.Errorf("read_only must NOT grant %q, got %v", banned, tools)
		}
	}
}

// Write-capable profiles get page writes and handoffs, but destructive and
// bulk operations stay privileged-only.
func TestProfileExtensionTools_ExecuteSafeGrantsWriteNotAdmin(t *testing.T) {
	for _, profile := range []CapabilityProfile{ProfileEditProject, ProfileExecuteSafe} {
		tools := ProfileExtensionTools(profile)
		for _, want := range []string{"memory_write_page", "memory_handoff_begin", "memory_handoff_accept", "mcpScript"} {
			if !containsTool(tools, want) {
				t.Errorf("%s should grant %q, got %v", profile, want, tools)
			}
		}
		for _, banned := range []string{"memory_delete_page", "memory_forget_sweep", "memory_lint", "memory_consolidate", "memory_auto_improve", "memory_install_self_routing"} {
			if containsTool(tools, banned) {
				t.Errorf("%s must NOT grant %q, got %v", profile, banned, tools)
			}
		}
	}
}

func TestProfileExtensionTools_PrivilegedGrantsEverything(t *testing.T) {
	tools := ProfileExtensionTools(ProfilePrivileged)
	for _, want := range []string{"memory_query", "memory_write_page", "memory_delete_page", "memory_forget_sweep", "memory_install_self_routing", "mcpScript"} {
		if !containsTool(tools, want) {
			t.Errorf("privileged should grant %q, got %v", want, tools)
		}
	}
}

// The returned slice is owned by the caller: mutating it must not corrupt the
// shared group backing arrays for later calls.
func TestProfileExtensionTools_ReturnsFreshSlice(t *testing.T) {
	first := ProfileExtensionTools(ProfileReadOnly)
	first[0] = "tampered"
	second := ProfileExtensionTools(ProfileReadOnly)
	if second[0] == "tampered" {
		t.Fatal("ProfileExtensionTools must not share backing arrays across calls")
	}
}

func TestGrantExtensionTools_AppendsToResolvedAllowlist(t *testing.T) {
	resolved := []string{"Read", "Write", "Bash"}
	got := GrantExtensionTools(ProfileExecuteSafe, resolved, nil)

	for _, want := range []string{"Read", "Write", "Bash", "memory_query", "memory_write_page"} {
		if !containsTool(got, want) {
			t.Errorf("granted list missing %q: %v", want, got)
		}
	}
	if containsTool(got, "memory_delete_page") {
		t.Errorf("execute_safe must not grant destructive memory tools: %v", got)
	}
}

func TestGrantExtensionTools_AgentDenylistWins(t *testing.T) {
	got := GrantExtensionTools(ProfileExecuteSafe, []string{"Read"}, []string{"memory_write_page", "Bash"})
	if containsTool(got, "memory_write_page") {
		t.Errorf("agent disallowed_tools must remove extension tools: %v", got)
	}
	if !containsTool(got, "memory_query") {
		t.Errorf("non-denied extension tools must survive: %v", got)
	}
}

// A name already present in the resolved allowlist must not be duplicated.
func TestGrantExtensionTools_NoDuplicates(t *testing.T) {
	got := GrantExtensionTools(ProfileReadOnly, []string{"memory_query", "Read"}, nil)
	count := 0
	for _, name := range got {
		if name == "memory_query" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("memory_query appears %d times in %v, want 1", count, got)
	}
	if !reflect.DeepEqual(got[:2], []string{"memory_query", "Read"}) {
		t.Fatalf("existing order must be preserved, got %v", got)
	}
}
