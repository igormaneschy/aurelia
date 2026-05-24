package planning

import (
	"strings"
	"testing"
)

func TestBuildPlanningPrompt_Minimal(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseSpecify,
		Status: StatusActive,
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Modo Plano") {
		t.Error("prompt must mention Modo Plano")
	}
	if !strings.Contains(prompt, "/home/user/project") {
		t.Error("prompt must include CWD")
	}
	if !strings.Contains(prompt, "Especificação") {
		t.Error("prompt must include phase Especificação")
	}
	if !strings.Contains(prompt, "Ativo") {
		t.Error("prompt must include status Ativo")
	}
	if !strings.Contains(prompt, "Contexto do projeto: Nenhum") {
		t.Error("prompt must say Contexto do projeto: Nenhum when ProjectCtx is nil")
	}
	if !strings.Contains(prompt, "Nenhum artefato materializado ainda") {
		t.Error("prompt must say Nenhum artefato when Materialized is empty")
	}
	if strings.Contains(prompt, "NeedsLayoutChoice") {
		t.Error("prompt must not mention NeedsLayoutChoice when flag is false")
	}
}

func TestBuildPlanningPrompt_WithProjectContext(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseDesign,
		Status: StatusActive,
		ProjectCtx: &ProjectContext{
			Layouts:           []string{"tlc"},
			Stacks:            []string{"go"},
			NeedsLayoutChoice: false,
		},
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Layouts detectados: tlc") {
		t.Errorf("prompt should list tlc layout, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Stacks: go") {
		t.Errorf("prompt should list go stack, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Escolha de layout pendente: não") {
		t.Errorf("prompt should say pendente: não, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Design") {
		t.Error("prompt must include phase Design")
	}
	if strings.Contains(prompt, "NeedsLayoutChoice") {
		t.Error("prompt must not mention NeedsLayoutChoice when flag is false")
	}
}

func TestBuildPlanningPrompt_WithArtifacts(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseTasks,
		Status: StatusActive,
		Materialized: []Artifact{
			{
				Path:      "/home/user/project/main.go",
				Phase:     PhaseDesign,
				Tool:      "Write",
				InsideCWD: true,
				Confirmed: true,
			},
			{
				Path:      "/tmp/scratch.md",
				Phase:     PhaseTasks,
				Tool:      "Edit",
				InsideCWD: false,
				Confirmed: false,
			},
		},
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "/home/user/project/main.go") {
		t.Error("prompt must list first artifact path")
	}
	if !strings.Contains(prompt, "/tmp/scratch.md") {
		t.Error("prompt must list second artifact path")
	}
	if !strings.Contains(prompt, "dentro do cwd: sim") {
		t.Error("prompt must mark first artifact as inside cwd")
	}
	if !strings.Contains(prompt, "dentro do cwd: não") {
		t.Error("prompt must mark second artifact as outside cwd")
	}
	if !strings.Contains(prompt, "confirmado: sim") {
		t.Error("prompt must mark first artifact as confirmed")
	}
	if !strings.Contains(prompt, "confirmado: não") {
		t.Error("prompt must mark second artifact as not confirmed")
	}
	if !strings.Contains(prompt, "Tarefas") {
		t.Error("prompt must include phase Tarefas")
	}
}

func TestBuildPlanningPrompt_NeedsLayoutChoice(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseSpecify,
		Status: StatusActive,
		ProjectCtx: &ProjectContext{
			Layouts:           []string{"tlc", "rfc"},
			Stacks:            []string{"go"},
			NeedsLayoutChoice: true,
		},
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Layouts detectados: tlc, rfc") {
		t.Errorf("prompt should list both layouts, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Escolha de layout pendente: sim") {
		t.Errorf("prompt should say pendente: sim, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "NeedsLayoutChoice") {
		t.Error("prompt should mention NeedsLayoutChoice when flag is true")
	}
}

func TestBuildPlanningPrompt_WithHandoffError(t *testing.T) {
	state := &State{
		CWD:              "/home/user/project",
		Phase:            PhaseReview,
		Status:           StatusAwaitingExec,
		LastHandoffError: "o arquivo main.go não foi encontrado",
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Erro do handoff anterior") {
		t.Error("prompt must include handoff error header")
	}
	if !strings.Contains(prompt, "o arquivo main.go não foi encontrado") {
		t.Error("prompt must include handoff error message")
	}
	if !strings.Contains(prompt, "Revisão") {
		t.Error("prompt must include phase Revisão")
	}
	if !strings.Contains(prompt, "Aguardando execução") {
		t.Error("prompt must include status Aguardando execução")
	}
}

func TestBuildPlanningPrompt_NoCancel(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseSpecify,
		Status: StatusActive,
	}

	prompt := BuildPlanningPrompt(state)

	// The prompt must NOT suggest using /cancel as a positive action.
	// If /cancel appears, it must only be in the negative instruction
	// "Não use /cancel como saída primária".
	if strings.Contains(prompt, "/cancel") {
		// Verify it's only in the "Não use" context — a prohibition, not a suggestion
		if !strings.Contains(prompt, "Não use /cancel") {
			t.Error("/cancel must only appear in the 'Não use' prohibition context")
		}
		// Count occurrences: should be exactly 1 (the "Não use" instruction)
		if strings.Count(prompt, "/cancel") != 1 {
			t.Errorf("expected exactly 1 occurrence of /cancel, got %d", strings.Count(prompt, "/cancel"))
		}
	}
}

func TestBuildPlanningPrompt_Deterministic(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseTasks,
		Status: StatusActive,
		ProjectCtx: &ProjectContext{
			Layouts:           []string{"tlc", "adr"},
			Stacks:            []string{"go", "node"},
			NeedsLayoutChoice: true,
		},
		Materialized: []Artifact{
			{Path: "file1.go", Phase: PhaseDesign, Tool: "Write", InsideCWD: true, Confirmed: true},
			{Path: "file2.md", Phase: PhaseTasks, Tool: "Edit", InsideCWD: false, Confirmed: false},
		},
		LastHandoffError: "erro de teste",
	}

	p1 := BuildPlanningPrompt(state)
	p2 := BuildPlanningPrompt(state)

	if p1 != p2 {
		t.Error("BuildPlanningPrompt must be deterministic: same state produced different output")
	}
}

func TestBuildPlanningPrompt_EmptyMaterialized(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseSpecify,
		Status: StatusActive,
		ProjectCtx: &ProjectContext{
			Layouts: []string{"tlc"},
			Stacks:  []string{"go"},
		},
		Materialized: []Artifact{},
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Nenhum artefato materializado ainda") {
		t.Error("prompt must say Nenhum artefato when Materialized is empty (not nil)")
	}
}

func TestBuildPlanningPrompt_NilLayoutsStacks(t *testing.T) {
	state := &State{
		CWD:    "/home/user/project",
		Phase:  PhaseDesign,
		Status: StatusActive,
		ProjectCtx: &ProjectContext{
			Layouts: nil,
			Stacks:  nil,
		},
	}

	prompt := BuildPlanningPrompt(state)

	if !strings.Contains(prompt, "Layouts detectados: Nenhum") {
		t.Error("prompt must say Nenhum for nil layouts")
	}
	if !strings.Contains(prompt, "Stacks: Nenhum") {
		t.Error("prompt must say Nenhum for nil stacks")
	}
	if !strings.Contains(prompt, "Escolha de layout pendente: não") {
		t.Error("prompt must say pendente: não when NeedsLayoutChoice is false")
	}
}
