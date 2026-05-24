package planning

import (
	"fmt"
	"strings"
)

// BuildPlanningPrompt constructs a system prompt segment for Plan Mode.
// It is injected into the system prompt when a planning state is active.
func BuildPlanningPrompt(state *State) string {
	var sb strings.Builder

	// 1. Header with CWD
	fmt.Fprintf(&sb, "Você está no Modo Plano para o projeto em %s.\n\n", state.CWD)

	// 2. Current phase and status
	fmt.Fprintf(&sb, "Fase atual: %s\n", translatePhase(state.Phase))
	fmt.Fprintf(&sb, "Status: %s\n\n", translateStatus(state.Status))

	// 3. Project context
	buildProjectContext(&sb, state.ProjectCtx)

	// 4. Materialized artifacts
	buildArtifactList(&sb, state.Materialized)
	sb.WriteString("\n")

	// 5. Last handoff error
	if state.LastHandoffError != "" {
		fmt.Fprintf(&sb, "Erro do handoff anterior:\n%s\n\n", state.LastHandoffError)
	}

	// 6. Instructions
	buildInstructions(&sb, state.ProjectCtx)

	return sb.String()
}

func buildProjectContext(sb *strings.Builder, ctx *ProjectContext) {
	if ctx == nil {
		sb.WriteString("Contexto do projeto: Nenhum\n\n")
		return
	}
	sb.WriteString("Contexto do projeto:\n")
	if len(ctx.Layouts) > 0 {
		fmt.Fprintf(sb, "- Layouts detectados: %s\n", strings.Join(ctx.Layouts, ", "))
	} else {
		sb.WriteString("- Layouts detectados: Nenhum\n")
	}
	if len(ctx.Stacks) > 0 {
		fmt.Fprintf(sb, "- Stacks: %s\n", strings.Join(ctx.Stacks, ", "))
	} else {
		sb.WriteString("- Stacks: Nenhum\n")
	}
	layoutChoice := "não"
	if ctx.NeedsLayoutChoice {
		layoutChoice = "sim"
	}
	fmt.Fprintf(sb, "- Escolha de layout pendente: %s\n\n", layoutChoice)
}

func buildArtifactList(sb *strings.Builder, artifacts []Artifact) {
	sb.WriteString("Artefatos materializados:\n")
	if len(artifacts) == 0 {
		sb.WriteString("- Nenhum artefato materializado ainda.\n")
		return
	}
	for _, a := range artifacts {
		insideCWD := "não"
		if a.InsideCWD {
			insideCWD = "sim"
		}
		confirmed := "não"
		if a.Confirmed {
			confirmed = "sim"
		}
		fmt.Fprintf(sb, "- %s (%s, %s, dentro do cwd: %s, confirmado: %s)\n",
			a.Path, translatePhase(a.Phase), a.Tool, insideCWD, confirmed)
	}
}

func buildInstructions(sb *strings.Builder, ctx *ProjectContext) {
	sb.WriteString("Instruções:\n")
	sb.WriteString("- Você pode materializar artefatos usando Write, Edit ou MultiEdit.\n")
	sb.WriteString("- Só prossiga para execução (handoff) após aprovação explícita do usuário.\n")
	sb.WriteString("- Não use /cancel como saída primária do Modo Plano.\n")
	if ctx != nil && ctx.NeedsLayoutChoice {
		sb.WriteString("- Se NeedsLayoutChoice for true, peça ao usuário para escolher um layout antes de materializar.\n")
	}
}

func translatePhase(p Phase) string {
	switch p {
	case PhaseSpecify:
		return "Especificação"
	case PhaseDesign:
		return "Design"
	case PhaseTasks:
		return "Tarefas"
	case PhaseReview:
		return "Revisão"
	default:
		return string(p)
	}
}

func translateStatus(s Status) string {
	switch s {
	case StatusActive:
		return "Ativo"
	case StatusAwaitingExec:
		return "Aguardando execução"
	case StatusCompleted:
		return "Concluído"
	case StatusCancelled:
		return "Cancelado"
	default:
		return string(s)
	}
}
