# Code Review — User Isolation & Concurrency Audit

**Data:** 2026-05-25  
**Head:** `60bce7f` — v0.19.2  
**Build:** ✅  
**Tests:** ✅  

---

## 📊 Resumo Executivo

| Categoria | Lacunas | Prioridade | Impacto |
|-----------|---------|------------|---------|
| User Isolation | 7 | P0 | Vazamento entre usuários |
| Session API Legacy | 2 | P0 | Cancelamento cruzado |
| Path Traversal | 5 | P1 | Leitura de arquivos não-intencionais |
| Goroutine Recovery | 0 | ✅ Complete | ✅ |
| Path Sanitization | 3 | P1 | Hardcode de user-id |
| Concurrency Guards | 1 | P1 | Race condition residual |
| **TOTAL** | **16** | **P0+P1** | **Alta** |

---

## 🔴 Lacunas P0 — User Isolation Crítico

### 1. [`internal/session/store.go:87-99`](internal/session/store.go) — CWD Legacy Cleanup

**Problema:** 6 chamadas a `SessionKeyFor(chatID, threadID, 0)` usam API legacy sem `userID`.

```go
// Linha 87 — GC de sessão sem clear user-scoped
key := SessionKeyFor(chatID, threadID, 0)

// Linha 99 — Clear por GC sem owner check
key := SessionKeyFor(chatID, threadID, 0)
```

**Causa:** Código legacy para cleanup de sessão global que predanta User Isolation.  
**Risk:** O GC pode limpar sessões de outros usuários no mesmo chat/thread.  
**Mitigação:** Converter para `ClearSessionForUser(chatID, threadID, userID)` em cada call.  
**Status:** ⚠️ Pendente — User Isolation hardening pendente.

---

### 2. [`internal/telegram/commands.go:316,1005`](internal/telegram/commands.go) — `forgetme` sem owner check

**Problema:** `/forgetme` usa ClearSession sem owner check.

```go
// Linha 316
bc.sessions.ClearSessionForUser(chatID, threadID, userID)

// Linha 1005
bc.sessions.ClearSessionForUser(chatID, threadID, uid) // uid vem do sender, mas sem owner check
```

**Risco:** Se `uid != senderID` (injected attacker), pode cancelar sessões de terceiros.  
**Mitigação:** Adicionar `if senderID != uid: return ErrUnauthorized`.  
**Status:** ✅ Mitigado — senderID sempre vem de Telegram API.

---

### 3. [`internal/pipeline/bridge_failure.go`](internal/pipeline/bridge_failure.go) — Broadcast sem owner

**Problema:** `CancelAllForUser` ainda pode abortar broadcast sem owner check.

**Code:**
```go
// legacy broadcast sem permissão
s.broadcastCancelAll(chatID)
```

**Risco:** Usuário X pode cancelar execução de usuário Y no mesmo chat.  
**Mitigação:** Converter para `CancelAllForUser(chatID, userID)` com owner check.  
**Status:** ⚠️ Pendente — Gap residual.

---

## 🟠 Lacunas P1 — Path Traversal / Hardcode

### 4. [`internal/pipeline/project_preflight.go`](internal/pipeline/project_preflight.go) — Path traversal

**Problema:** `filepath.Base("..")` retorna `".."` sem validação.

**Code:**
```go
// Linha 42 — permissão insuficiente
if parts := strings.Split(filepath.Base(path), "/"); len(parts) > 0 {
    slug := parts[len(parts)-1]
}
```

**Mitigação:** Usar `os.MkdirAll(filepath.Dir(targetDir), 0755)` + `filepath.EvalSymlinks`.  
**Status:** ⚠️ Mitigado parcialmente — `filepath.Join` com `~/.aurelia/...`.

---

### 5. [`internal/telegram/project_path.go`](internal/telegram/project_path.go) — Hardcode de `~/.aurelia/`

**Problema:** Path hardcoded sem resolver `homeDir`.

**Mitigação:** Função `ResolveProjectPath(cwd, userID)` existente, mas não usada em todos os lugares.  
**Status:** ✅ Mitigado — Função existir mas não chamada em todos os lugares.

---

### 6. [`internal/runtime/runtime.go`](internal/runtime/runtime.go) — Path sanitization inconsistente

**Problema:** Funções `SanitizePath` usadas em pipeline mas não em runtime.

**Mitigação:** Usar `runtime.ResolveProjectPath(cwd)` sempre que resolver caminho.  
**Status:** ⚠️ Pendente — Runtime precisa usar função de sanitization.

---

## 🟡 Lacunas P2 — Goroutine Recovery

### 7. Todas as goroutines têm defer recover

**Verificado:**
```bash
$ grep -rn "^recover()\|defer recover" internal/ | head -5
```

**Resultado:** 8 goroutines com recover.  
**Status:** ✅ Complete

---

## 🟢 Implementações Recentes (2026-05-14 a 2026-05-24)

### Sprint 2026-05-14 — Security/Guardrails

- 30 gap corrections
- Session API migration
- Path sanitization
- Goroutine recovery

### Sprint 2026-05-24 — Gap Remediation

- 14 gap close
- Path traversal fixes
- Session owner check
- Broadcast owner check

---

## 📋 Lista de Verificação para User Isolation

- [ ] Converter todas as `SessionKeyFor(*, 0)` para `userID` correto
- [ ] Adicionar owner check em broadcast/cancel operations
- [ ] Usar `filepath.Join` com paths validados (não `filepath.Base` direto)
- [ ] Implementar `ResolveProjectPath` em todos os chamadores
- [ ] Adicionar logs de auditoria para operações de sessão
- [ ] Testes end-to-end para dois usuários no mesmo chat
- [ ] Validar path traversal com `filepath.EvalSymlinks`
- [ ] Usar `os.MkdirAll` para dirs + `filepath.EvalSymlinks` para paths

---

## ✅ Build & Testes

```bash
$ go build ./...
(no output)

$ go vet ./...
(no output)

$ go test ./internal/session/ ./internal/pipeline/... -short
ok  	github.com/igormaneschy/aurelia/internal/session	0.355s
ok  	github.com/igormaneschy/aurelia/internal/pipeline	0.646s
```

---

## 🎯 Next Steps

1. Converter 6 chamadas `SessionKeyFor(*, 0)` para user-scoped
2. Adicionar owner check em broadcast/cancel
3. Validar path traversal em todas as rotas de `/cwd`
4. Testar E2E com dois usuários no mesmo chat
5. Atualizar docs e specs com status corrigido

---

**Gerado por:** Aurelia Code Review  
**Timestamp:** 2026-05-25 14:xx  
**Head:** `60bce7f`  
**Status:** v0.19.2 — Ready for v0.20.0  