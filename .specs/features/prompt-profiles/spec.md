# Prompt Profiles — Unificar `/mode`, `/agents` e `@profile`

**Status:** 🟡 Parcial — MVP semantics implementado em v0.21.0; Phase 1 (`internal/profiles` abstraction) pendente  
**Roadmap step:** pós Session/Profile Operability; antes de multi-engine routing  
**Depende de:** User Isolation, Session/Profile Operability, Security Guard-Rails, Bridge Adapter Interface  
**Substitui/realinha:** `Mode Profiles` como comportamento persistente e `internal/agents` como catálogo de perfis de injeção de contexto, sem transformar Aurelia em runtime de execução.

---

## 1. Product Thesis

Aurelia é a **camada de personalidade, contexto e UX** acima de harnesses/SDKs de execução. Hoje o harness concreto é o PI SDK; futuramente haverá outros SDKs/adapters. A Aurelia não deve reimplementar execução, skills, task decomposition, model runtime, sessão ou ferramentas do SDK. Ela deve empacotar o pedido com identidade, memória operacional, contexto de conversa/projeto e um perfil complementar, então delegar execução para o harness.

```text
Telegram / future TUI / cron
        ↓
Aurelia Personality Layer
- persona e identidade
- memória operacional e continuidade
- escopo usuário/chat/tópico/projeto
- seleção de Prompt Profile
- policy/audit/guard-rails
        ↓
Harness / SDK Adapter
- PI SDK hoje
- futuros SDKs depois
        ↓
Modelo, tools, sessões, skills nativas, execução
```

**Conceito central:** `/mode`, `/agents` e `@agent` são formas diferentes de selecionar **Prompt Profiles** — arquivos/configurações que a Aurelia injeta como contexto complementar no pedido enviado ao SDK. Eles não são agentes executores independentes.

---

## 2. Problem Statement

O código atual tem três conceitos parcialmente sobrepostos:

1. **Mode Profiles** (`Profile.ActiveMode`, `mode_<name>.md`, `/mode`) — estado persistente do usuário que injeta um overlay comportamental.
2. **Agents** (`~/.aurelia/agents/*.md`, `/agents`, `@name`) — registry markdown com prompt, modelo, tools, cwd e roteamento.
3. **SDK-native agents/skills** — recursos que pertencem ao PI SDK ou a futuros harnesses.

Essa separação criou ambiguidades de produto:

- Se `/mode developer` está ativo e o usuário chama `@researcher`, quem vence?
- Se `mode_developer.md` e `agents/coder.md` são ambos prompt injections, por que existem dois sistemas?
- `/agents` lista “agents”, mas a Aurelia não deveria executar agentes; o SDK executa.
- `internal/agents` tem campos de execução (`model`, `allowed_tools`, `cwd`) e campos de prompt no mesmo conceito, o que tende a duplicar responsabilidades do SDK.
- O caminho para multi-SDK precisa selecionar harness/profile de forma explícita, sem acoplar “agent” ao PI.

A solução é unificar a semântica em **Prompt Profiles**:

```text
/mode <profile>  = define o Prompt Profile padrão do usuário/conversa
@<profile>       = usa um Prompt Profile one-shot para esta mensagem
/agents          = catálogo compatível que lista Prompt Profiles disponíveis
```

---

## 3. Prior Lessons Applied

- **Delegate to PI SDK / PROJECT.md boundary** → Aurelia não deve reconstruir execução, skills, compaction, runtime de tools ou model routing; a spec define profiles como contexto complementar e mantém execução no harness.
- **Auto-Skills discarded** → criação/carregamento de skills nativas é responsabilidade do SDK; Prompt Profiles podem ser PI-compatible, mas a Aurelia os usa como product-layer context, não como skill runtime.
- **Agent Comms discarded** → worker orchestration pertence ao SDK; profiles não criam comunicação entre agentes.
- **Slash Commands MUST Be Registered via `bot.Handle`** → qualquer novo comando/alias de slash desta spec deve ter handler primário explícito, menu e `/help`.
- **Command Matching Must Separate Slash From Natural Language** → aliases naturais de `/mode` e `/agents` devem ser exact-only para evitar interceptar conversas normais.
- **Telegram thread routing explicit** → respostas de `/mode`, `/agents` e comandos derivados devem usar `ThreadID` sempre.
- **Model/metadata disclosure review** → listagens públicas em grupos devem evitar expor modelo, cwd, tool policy, MCP servers e paths.

---

## 4. Goals

- [ ] Introduzir o conceito canônico **Prompt Profile** no produto e no código.
- [ ] Tratar `mode` como **default Prompt Profile persistente**, não como uma camada concorrente.
- [ ] Tratar `@name` como **override one-shot de Prompt Profile** para a mensagem atual.
- [ ] Manter `/agents` por compatibilidade, mas mudar a UX para “Perfis disponíveis”.
- [ ] Resolver precedência com uma regra única: `@profile` explícito > profile ativo via `/mode` > `general`.
- [ ] Evitar composição de overlays fortes por padrão: cada execução tem exatamente um effective prompt profile além da persona/memória/Telegram context.
- [ ] Separar campos de **context injection** de campos de **harness execution hints**.
- [ ] Preparar schema para multi-harness sem implementar novo SDK neste sprint.
- [ ] Preservar compatibilidade com agentes markdown existentes em `~/.aurelia/agents/*.md`.
- [ ] Reduzir vazamento de metadata em `/agents`/catálogo.

---

## 5. Non-Goals / Out of Scope

- Implementar um segundo SDK/harness concreto.
- Migrar arquivos do usuário para `~/.pi/agent`.
- Implementar criação automática de skills.
- Implementar worker orchestration, agent-to-agent comms ou task decomposition na Aurelia.
- Remover imediatamente `internal/agents` ou quebrar `@agent` existente.
- Trocar o armazenamento físico de todos os arquivos existentes no MVP.
- Resetar sessão PI ao trocar profile/mode.
- Adicionar dependências novas sem aprovação explícita.

---

## 6. Vocabulary and Product Model

### 6.1 Persona

Identidade estável da Aurelia e do usuário.

- Fonte atual: `IDENTITY.md`, `SOUL.md`, `USER.md`.
- Duração: permanente.
- Função: “quem é Aurelia” e “quem é o usuário”.
- Sempre presente.

### 6.2 Prompt Profile

Preset de contexto/instruções complementares que a Aurelia injeta no pedido antes de enviar ao harness.

- Exemplos: `general`, `developer`, `researcher`, `coder`, `reviewer`, `prospector`.
- Duração: persistente via `/mode`, ou one-shot via `@profile`.
- Função: “como empacotar este pedido”.
- Não executa sozinho.

### 6.3 Harness

Runtime/SDK executor.

- Exemplo atual: `pi`.
- Exemplos futuros: `codex`, `claude-code`, `opencode`, `custom`.
- Função: executar modelo/tools/sessão.

### 6.4 Agent compatibility term

“Agent” continua existindo como termo de compatibilidade para arquivos e comandos atuais, mas a UX deve explicar que um agent da Aurelia é um **Prompt Profile invocável**.

---

## 7. Architecture Decision

### Chosen approach: one effective Prompt Profile per execution

Para cada mensagem, a Aurelia resolve exatamente um `effectiveProfile`:

```go
effectiveProfile = explicitMentionProfile ?? activeDefaultProfile ?? generalProfile
```

Depois monta o prompt:

```text
Runtime Identity
Base Persona
Owner/project docs when applicable
Telegram/context instructions
Security boundaries
Continuity/memory sections
Effective Prompt Profile
User task
```

**Importante:** o Prompt Profile efetivo substitui o “mode overlay” para aquela execução. Não há composição padrão de `/mode developer` + `@researcher`. Se `@researcher` for usado, ele vence naquela mensagem. Isso evita concorrência conceitual e prompts contraditórios.

### Why not compose mode + agent?

Composição aumenta ambiguidade:

```text
/mode developer + @researcher
```

Poderia significar researcher com viés developer, developer com ferramentas researcher, ou duas instruções conflitantes. Para manter produto simples:

- `/mode` seleciona o profile default.
- `@profile` substitui o default só nesta mensagem.
- Persona e memória continuam sempre.

### Future extension

P2 pode permitir composição explícita:

```text
@researcher --with developer compare SDKs
```

Mas não entra no MVP.

---

## 8. Data Model

### 8.1 Profile metadata

Criar um tipo canônico em package apropriado (`internal/profiles` recomendado):

```go
type PromptProfile struct {
    Name        string
    Description string
    Prompt      string

    // Product classification
    Kind        ProfileKind // builtin | global | user | legacy_agent
    Source      ProfileSource

    // Optional execution hints passed to harness adapter when supported.
    Harness     string // "pi" default; future adapters use this
    Model       string
    Cwd         string
    CapabilityProfile string
    AllowedTools    []string
    DisallowedTools []string
    MaxTurns     int
    ToolBudget   int

    // Safe display controls
    Public       bool
    Tags         []string
}
```

### 8.2 Storage compatibility

MVP reads existing files:

```text
~/.aurelia/agents/*.md                     # legacy/global profiles
~/.aurelia/users/<id>/personas/mode_*.md   # legacy mode overlays
```

Recommended future canonical layout:

```text
~/.aurelia/profiles/<name>.md              # global prompt profiles
~/.aurelia/users/<id>/profiles/<name>.md   # user-private prompt profiles, P2
```

MVP does **not** need to migrate files. It may create an adapter/loader that normalizes both legacy sources into `PromptProfile`.

### 8.3 Profile frontmatter

Canonical profile markdown:

```yaml
---
name: coder
description: Empacota pedidos de implementação técnica para o harness executar.
kind: prompt_profile
harness: pi
model: auto
capability_profile: edit_project
allowed_tools: [Read, Write, Edit, Bash, Grep, Glob, LS]
public: true
tags: [developer, code]
---

# Coder Profile

Você está empacotando um pedido de implementação. Seja objetivo, preserve contexto,
peça validação quando necessário e entregue instruções claras para o SDK executor.
```

Compatibility mapping from old `agents.Agent`:

| Legacy field | PromptProfile field | Notes |
|---|---|---|
| `name` | `Name` | same |
| `description` | `Description` | same |
| body | `Prompt` | same |
| `model` | `Model` | execution hint, not identity |
| `cwd` | `Cwd` | execution hint; hidden from public listing |
| `capability_profile` | `CapabilityProfile` | guard-rail input |
| `allowed_tools` | `AllowedTools` | harness/tool policy hint |
| `disallowed_tools` | `DisallowedTools` | harness/tool policy hint |
| `tool_budget` | `ToolBudget` | monitoring hint |
| `schedule` | unchanged/outside profile semantics | cron-owned; do not promote as core profile field |

### 8.4 User profile field

Current:

```go
ActiveMode string `json:"active_mode,omitempty"`
```

MVP compatibility:

- Keep `active_mode` JSON field to avoid migration.
- Treat it as active default Prompt Profile.
- Add accessor names in code such as `ActivePromptProfile()` / `SetActivePromptProfile()`.

Future optional field:

```go
ActiveProfile string `json:"active_profile,omitempty"`
```

If introduced, migration must map `active_mode` → `active_profile` and continue reading old JSON.

---

## 9. Command Semantics

### 9.1 `/mode`

`/mode` remains the command to set the **default Prompt Profile**.

#### UX copy

```text
Perfil ativo: developer

Perfis alteram como a Aurelia empacota seu pedido para o SDK.
Use @perfil para aplicar outro perfil só na próxima mensagem.
```

#### Commands

| Command | Behavior |
|---|---|
| `/mode` | show active default profile and short explanation |
| `/mode <name>` | set active default profile if valid |
| `/modo <name>` | Portuguese alias |
| `/mode general` | clear active profile / set default `general` |
| `/mode auto` | alias for `general` in MVP; no auto-routing mode yet |
| `/mode explain <name>` | P1/P2: show profile summary and whether it is default or one-shot only |

#### Acceptance Criteria

1. WHEN user sends `/mode developer` THEN Aurelia SHALL set active default profile to `developer`.
2. WHEN user sends `/mode general`, `/mode geral` or `/mode auto` THEN Aurelia SHALL clear active default profile and display `general`.
3. WHEN user sends `/mode` THEN Aurelia SHALL show current effective default profile and explain that `@profile` overrides only one message.
4. WHEN user sends `/mode <unknown>` THEN Aurelia SHALL say the profile was not found and suggest `/agents`.
5. WHEN profile store is unavailable THEN Aurelia SHALL return a clear user-facing setup/storage message, without local paths.
6. WHEN profile changes THEN Aurelia SHALL NOT reset the SDK session.
7. WHEN slash command runs in group/topic THEN response SHALL stay in the same `ThreadID`.
8. WHEN natural-language aliases are supported THEN they SHALL be exact-only.

---

### 9.2 `/agents`

`/agents` remains for compatibility but becomes the **Prompt Profile catalog**.

#### UX copy

```text
Perfis disponíveis
Perfil ativo: developer

@coder — implementação e mudanças em código
@researcher — pesquisa, comparação e síntese
@general — conversa geral

Use:
- /mode coder para tornar um perfil padrão
- @researcher <pedido> para usar uma vez
```

#### Display policy

| Context | Show name | Show description | Show model | Show cwd/tools/MCP | Show prompt body |
|---|---:|---:|---:|---:|---:|
| Owner private | yes | yes | optional verbose | optional verbose | no by default |
| Owner group/topic | yes | yes | no by default | no | no |
| Non-owner authorized | yes | yes if public | no | no | no |

#### Acceptance Criteria

1. WHEN `/agents` runs THEN title SHALL be `Perfis disponíveis` or include `Prompt Profiles` concept, not imply Aurelia executes workers itself.
2. WHEN active default profile exists THEN `/agents` SHALL show it.
3. WHEN no legacy agents exist THEN `/agents` SHALL still show builtin profiles (`general`, `developer`, `researcher`) and active profile state.
4. WHEN a profile has model/cwd/tool metadata THEN default `/agents` output SHALL hide it in groups and for non-owner users.
5. WHEN owner requests verbose listing in private chat (`/agents verbose` P1) THEN Aurelia MAY show model/capability/profile hints, but SHALL still hide prompt body unless explicitly requested by owner.
6. WHEN natural-language aliases like `lista agents` are kept THEN they SHALL be exact-only.
7. WHEN `/agents` runs in topic THEN response SHALL stay in the same topic.

---

### 9.3 `@profile`

`@name` becomes a one-shot profile selector.

#### Semantics

```text
/mode developer
@researcher compare PI SDK e Codex SDK
```

Effective profile for that turn: `researcher`. The active default `developer` remains unchanged for future messages.

#### Acceptance Criteria

1. WHEN message starts with `@coder ` and `coder` exists THEN Aurelia SHALL use `coder` as `effectiveProfile` for that message only.
2. WHEN `@coder` is used THEN persisted active default profile SHALL NOT change.
3. WHEN `@unknown` is used THEN Aurelia SHALL either pass through to SDK as normal text or return a clear unknown-profile message; choose one behavior and test it. Recommended: clear unknown-profile message when the token is at message start.
4. WHEN explicit profile is used THEN prompt builder SHALL inject only that profile, not active default profile.
5. WHEN explicit profile has harness/model/tool hints THEN those hints SHALL be passed through existing request options only when supported by current harness.

---

## 10. Prompt Assembly Contract

### 10.1 Current problem

Current prompt assembly injects agent prompt before persona and mode overlay inside persona. This can compose multiple behavioral overlays.

### 10.2 Required behavior

After this feature, prompt assembly SHALL receive one resolved `effectiveProfile` and inject it once.

Recommended section placement:

```text
# Runtime Identity
# Canonical Persona
# Telegram Context
# Security Boundaries
# Continuity / Memory
# Active Prompt Profile: <name>
<profile prompt>
```

Rationale:

- Persona remains identity.
- Security remains non-negotiable.
- Profile is close to final task-specific steering but must not override security.

### 10.3 Acceptance Criteria

1. WHEN active default is `developer` and message has no `@profile` THEN prompt SHALL contain exactly one `Active Prompt Profile: developer` section.
2. WHEN active default is `developer` and message starts `@researcher` THEN prompt SHALL contain exactly one `Active Prompt Profile: researcher` section and SHALL NOT contain `developer` profile body.
3. WHEN profile file is missing THEN fallback SHALL be `general` or no profile section, with log entry but no user-visible storage path.
4. WHEN profile prompt is empty THEN Aurelia SHALL treat it as unavailable and use fallback.
5. WHEN profile name is invalid THEN resolver SHALL reject before path construction.
6. WHEN SDK/harness also loads native agent files THEN Aurelia SHALL avoid injecting duplicate content unless explicitly configured.

---

## 11. Built-in Profiles

MVP SHOULD define builtins even when no files exist.

### 11.1 `general`

Purpose: balanced assistant behavior.

Prompt principles:

- conversational, concise, helpful;
- no special execution assumptions;
- ask clarifying questions only when necessary.

### 11.2 `developer`

Purpose: software/product engineering context packaging.

Prompt principles:

- prioritize architecture, risks, validation and maintainability;
- preserve scope discipline;
- prefer concrete file/test references when code context exists;
- do not execute outside harness capabilities.

### 11.3 `researcher`

Purpose: exploration, comparison, synthesis.

Prompt principles:

- distinguish evidence, inference and uncertainty;
- compare alternatives;
- cite sources when web/tool access exists;
- summarize trade-offs and recommendation.

Builtins can be implemented as embedded defaults or generated fallback bodies. User files may override only if explicitly allowed and scoped.

---

## 12. Harness and Multi-SDK Preparation

Prompt Profiles MAY include `harness` as an execution hint, but MVP only supports `pi`.

### Acceptance Criteria

1. WHEN profile has no `harness` THEN Aurelia SHALL use configured default harness (`pi` today).
2. WHEN profile has `harness: pi` THEN current behavior SHALL remain unchanged.
3. WHEN profile has unsupported harness THEN Aurelia SHALL return a clear message: `Harness "x" ainda não está disponível.`
4. WHEN Bridge Adapter Interface lands THEN profile harness selection SHALL map to `engine.Engine` adapter selection, not to `internal/bridge` directly.
5. WHEN future SDK supports native agent/profile files THEN adapter MAY translate `PromptProfile` to SDK-native config rather than textual injection, but user semantics SHALL remain unchanged.

---

## 13. Migration / Compatibility Plan

### Phase 0: UX and semantics only

- Keep `internal/agents` package and `agents.Agent` type.
- Update `/agents` output copy to “Perfis”.
- Update `/mode` copy to “Perfil ativo”.
- Enforce `@profile > /mode > general` in prompt resolution.
- Stop injecting both legacy agent prompt and mode overlay together.

### Phase 1: Introduce `internal/profiles`

- Create profile resolver that adapts:
  - builtins;
  - legacy `~/.aurelia/agents/*.md`;
  - legacy `mode_<name>.md` files;
  - future canonical profiles.
- Pipeline depends on `profiles.PromptProfile`, not `agents.Agent`, for prompt assembly.
- Legacy `agents.Registry` may remain as loader behind the resolver.

### Phase 2: Canonical storage and docs

- Add docs for `~/.aurelia/profiles/*.md`.
- Optionally add migration script or lazy compatibility loader.
- README explains `/mode`, `/agents`, `@profile` as one concept.

### Phase 3: Multi-harness routing

- After Bridge Adapter Interface: route `profile.Harness` to engine adapter.
- Keep semantics stable.

---

## 14. User Stories

### P0: Unified effective profile resolution ⭐ MVP

**User Story:** Como usuário, quero que `/mode` e `@profile` não briguem entre si; quero uma regra simples para saber qual perfil será usado.

**Why P0:** Essa é a correção conceitual central.

**Acceptance Criteria:**

1. WHEN no active profile and no `@profile` THEN effective profile SHALL be `general`.
2. WHEN active profile is `developer` and no `@profile` THEN effective profile SHALL be `developer`.
3. WHEN active profile is `developer` and message starts `@researcher` THEN effective profile SHALL be `researcher` for that turn.
4. WHEN a one-shot profile is used THEN next message without `@profile` SHALL return to active default.
5. WHEN profile resolution chooses a profile THEN runlog SHOULD record profile name when feasible.

**Independent Test:** Pipeline prompt builder test with fake profile store verifies each resolution case and checks exactly one profile section.

---

### P0: Product copy realignment ⭐ MVP

**User Story:** Como usuário, quero entender que os “agents” da Aurelia são perfis de contexto, não workers executando por fora do SDK.

**Acceptance Criteria:**

1. WHEN `/agents` is shown THEN it SHALL use `Perfis disponíveis` or equivalent.
2. WHEN `/mode` is shown THEN it SHALL use `Perfil ativo` or equivalent.
3. WHEN `/help` is shown THEN it SHALL explain:
   - `/mode <perfil>` defines default profile;
   - `@perfil <pedido>` uses a profile once;
   - `/agents` lists profiles.
4. WHEN README is updated THEN command table SHALL include `/mode` and explain `@profile`.

**Independent Test:** Unit tests over `helpMessage()` and `/agents` formatting.

---

### P0: Metadata-safe catalog ⭐ MVP

**User Story:** Como owner, quero listar perfis sem vazar detalhes operacionais sensíveis em grupos.

**Acceptance Criteria:**

1. WHEN `/agents` runs in group/topic THEN response SHALL hide model, cwd, allowed tools, disallowed tools, MCP servers and prompt body.
2. WHEN non-owner authorized user runs `/agents` THEN response SHALL hide execution metadata.
3. WHEN owner runs `/agents` in private chat THEN default output SHALL still hide sensitive metadata; verbose mode MAY show safe execution hints.
4. WHEN a profile is marked `public: false` THEN non-owner listing SHALL hide it or show only a generic unavailable marker. MVP may treat all legacy profiles as public name+description only.

**Independent Test:** Formatting test with owner/non-owner + private/group variants.

---

### P1: `internal/profiles` resolver

**User Story:** Como desenvolvedor, quero uma abstração única para carregar builtins, legacy agents e mode overlays como Prompt Profiles.

**Acceptance Criteria:**

1. WHEN resolver is created THEN it SHALL expose `Get(name)`, `List(userID)`, `ResolveEffective(userID, text, activeDefault)` or equivalent.
2. WHEN legacy agent exists THEN resolver SHALL return it as `Kind=legacy_agent` profile.
3. WHEN legacy `mode_developer.md` exists THEN resolver SHALL merge/override builtin `developer` prompt according to documented precedence.
4. WHEN duplicate profile names exist THEN resolver SHALL use deterministic precedence and log collisions. Recommended precedence: user-private > canonical global > legacy agent > builtin, except builtins `general/developer/researcher` cannot be silently deleted.
5. WHEN file has invalid frontmatter THEN resolver SHALL skip it with log, not crash `/agents`.

**Independent Test:** Resolver unit tests with temp dirs for duplicate, invalid, missing and builtin-only cases.

---

### P1: One-shot profile invocation

**User Story:** Como usuário, quero usar `@researcher` só para uma pergunta sem mudar meu perfil padrão.

**Acceptance Criteria:**

1. WHEN message starts with `@name` THEN parser SHALL extract `name` as profile candidate and strip prefix from user task if found.
2. WHEN profile exists THEN pipeline SHALL send stripped task to harness.
3. WHEN profile does not exist THEN behavior SHALL be deterministic and tested. Recommended: reply `Perfil @name não encontrado. Use /agents.` and do not call harness.
4. WHEN `@name` appears in the middle of text THEN parser SHALL NOT treat it as profile invocation.
5. WHEN `@name` is followed by punctuation without space THEN parser SHALL NOT invoke unless exact syntax is explicitly supported.

**Independent Test:** Parser and pipeline tests for start/middle/unknown/strip behavior.

---

### P2: Profile explain and verbose commands

**User Story:** Como usuário, quero entender o que um perfil muda antes de usá-lo.

**Acceptance Criteria:**

1. WHEN `/mode explain developer` THEN Aurelia SHALL show description, usage and safe summary of behavior.
2. WHEN `/agents explain coder` THEN Aurelia SHALL show safe summary and example invocation.
3. WHEN summary would include sensitive metadata THEN hide it unless owner private verbose mode.

---

## 15. Edge Cases and Risks

- **Unknown `@name` at message start:** Avoid silently sending confusing text to SDK. Recommended: local error + `/agents` hint.
- **Profile prompt missing:** fallback to builtin or general; no user-visible filesystem path.
- **Duplicate `developer` in legacy agents and mode overlay:** deterministic precedence required; log collision.
- **Group disclosure:** default catalog hides execution metadata.
- **Mode punctuation:** trim trailing punctuation in `/mode <name>` targets before lookup.
- **Natural language false positives:** natural aliases must be exact-only.
- **Harness unsupported:** fail closed with clear message; do not route to default silently if profile explicitly requested unsupported harness.
- **Profile with dangerous tools:** existing Security Guard-Rails still intersect tools and capability profiles; profile cannot override security policy.
- **SDK-native agent duplication:** if adapter uses SDK native agent config in future, do not also inject same prompt text unless configured.
- **Cron jobs:** scheduled legacy agents may still use `schedule`; this spec does not redesign cron scheduling. Cron should use effective profile semantics only after a separate review.

---

## 16. Testing Strategy

| Layer | Behavior | Pattern |
|---|---|---|
| Unit | profile resolution precedence | new `internal/profiles/*_test.go` |
| Unit | `/mode` target normalization and punctuation | `internal/telegram/commands_test.go` |
| Unit | `/agents` safe formatting owner/private/group | `internal/telegram/commands_test.go` |
| Unit | exact-only natural command matching | `internal/telegram/commands_test.go` |
| Unit | `@profile` parser strips only start token | pipeline or profiles parser test |
| Integration-ish | prompt builder injects exactly one effective profile | `internal/pipeline/prompt_builder_test.go` |
| Security | non-owner/group catalog hides model/cwd/tools | telegram formatting tests |
| Manual | Telegram topic `/mode`, `/agents`, `@profile` replies stay in topic | live daemon validation |

Validation commands:

```bash
go test ./internal/telegram/... -run 'Test.*Mode|Test.*Agents|Test.*Profile' -v
go test ./internal/pipeline/... -run 'TestBuildSystemPrompt.*Profile|TestRoute.*Profile' -v
go test ./internal/profiles/... -v
go test ./... -short
go vet ./...
```

---

## 17. Success Criteria

- [ ] Product language explains one concept: Prompt Profiles.
- [ ] `/mode` sets default Prompt Profile.
- [ ] `@profile` applies one-shot override and does not mutate default.
- [ ] `/agents` lists Prompt Profiles and shows active default.
- [ ] Prompt builder injects exactly one effective profile section per execution.
- [ ] No default composition of legacy agent prompt + mode overlay.
- [ ] Metadata-safe catalog behavior in groups/non-owner contexts.
- [ ] Natural-language command matches are exact-only where applicable.
- [ ] Existing `@agent` workflows remain compatible.
- [ ] Builtin `general`, `developer`, `researcher` profiles are available even with no files.
- [ ] Unsupported future harness values fail closed with clear user message.
- [ ] `go test ./... -short` and `go vet ./...` pass after implementation.

---

## 18. Open Questions

1. Should active default profile be scoped per user globally, or per conversation/thread? Current `Profile.ActiveMode` is per-user. Product may prefer per-chat/thread for group workflows.
2. Should `/mode auto` remain alias for `general`, or become intent-based profile auto-detection in P2?
3. Should legacy `schedule` agents remain under `/agents`, or move to `/cron agents` / `/schedules` to avoid mixing scheduled automations with prompt profiles?
4. Should owner-private verbose mode show model/tool hints, or should catalog never reveal execution metadata through Telegram?
5. Should canonical storage be introduced immediately (`~/.aurelia/profiles/`) or only after Phase 0 validates UX?

---

## 19. Recommended Implementation Plan

1. **Phase 0 / MVP semantics:** update copy, exact-only matching, metadata-safe `/agents`, punctuation handling, and `@profile > /mode > general` prompt resolution using current packages.
2. **Phase 1 / abstraction:** add `internal/profiles` and migrate prompt builder dependency from `agents.Agent` to `profiles.PromptProfile` while preserving legacy loader.
3. **Phase 2 / storage/docs:** add canonical profile docs and optional storage path.
4. **Phase 3 / harness routing:** integrate with `engine.Engine` once Bridge Adapter Interface exists.

This keeps the first release small and product-correct while avoiding a premature migration of user files or SDK-native agent systems.
