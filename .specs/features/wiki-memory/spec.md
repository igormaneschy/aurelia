# Wiki Memory Gateway — Superseded Specification

**Roadmap step:** 6  
**Status:** 🗑️ Superseded / descartada como implementação ativa  
**Superseded by:** PI + `ai-memory` MCP direto  
**Decision date:** 2026-06-02

## Decision

Aurelia **não implementará** um Wiki MCP Gateway interno.

A memória Wiki transversal passa a ser responsabilidade do **PI usando o MCP `ai-memory`** já disponível no ambiente do operador. Isso mantém a regra arquitetural do projeto: quando uma capacidade já existe no PI ou em extensão MCP usada diretamente pelo PI, Aurelia adapta/orquestra em vez de reimplementar.

```text
Telegram / Cron / Aurelia UX
        ↓
Aurelia Product Layer
identidade · persona · UX · workflows · memória operacional · políticas · continuidade
        ↓
PI SDK
reasoning · tools · sessions · MCP tools · agent runtime
        ↓
ai-memory MCP
Wiki memory transversal
```

## New Boundary

### PI + ai-memory MCP owns

- Wiki memory transversal;
- busca, leitura, escrita, lint/consolidação e handoff de páginas Wiki;
- interoperabilidade entre PI direto, PI Code/opencode e outros clientes MCP;
- índice/search/metadata próprios do `ai-memory`.

### Aurelia owns

- Telegram UX e comandos;
- identidade/persona do daemon e do usuário;
- cron/scheduling;
- project binding e `/cwd` como contexto declarativo;
- guard-rails, redaction e audit quando Aurelia processa dados;
- continuity, run journal e orchestration;
- memória operacional/produto necessária ao prompt Telegram, sem tentar ser a Wiki transversal canônica.

## Explicitly Not Implemented in Aurelia

Os seguintes itens da spec original foram descartados:

- servidor MCP interno para Wiki;
- pacote `internal/mcp` para Wiki;
- ferramentas `wiki_query`, `wiki_save`, `wiki_ingest`, `wiki_lint`, `wiki_status`;
- SQLite/FTS/BM25 próprio para Wiki;
- receipts de escrita externa via MCP interno;
- `query-before-inject` próprio de Wiki no Aurelia;
- Wiki como fonte canônica textual do Aurelia.

## Implications for Dependent Specs

- `project-memory` continua válido apenas como **memória operacional escopada** usada por Aurelia no prompt/UX/continuidade.
- `learning-nudge` não deve escrever através de uma Wiki interna. O MVP deve produzir sugestões/updates escopados e persistir apenas memória operacional local quando necessário; qualquer escrita em Wiki transversal deve acontecer via PI/`ai-memory` por caminho explicitamente configurado e verificado.
- `auto-skills` continua Aurelia-native e PI-compatible (`SKILL.md`), com criação explícita pelo usuário.

## Historical Note

A spec original planeava uma Wiki local-first nativa do Aurelia, exposta por MCP para PI, PI Code/opencode e futuros clientes. Essa direção foi abandonada porque o `ai-memory` MCP já fornece essa camada diretamente no PI, tornando o gateway interno duplicativo e contrário ao princípio de não competir com capacidades do PI SDK/MCP.
