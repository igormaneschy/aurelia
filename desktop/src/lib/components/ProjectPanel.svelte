<script lang="ts">
  import { open } from "@tauri-apps/plugin-dialog";
  import type { ModelEntry, ProjectState } from "../types";

  export let project: ProjectState | null = null;
  export let models: ModelEntry[] = [];
  export let modelsLoading = false;
  export let loading = false;
  export let running = false;

  export let onSetCwd: (path: string) => void | Promise<void> = () => {};
  export let onClearCwd: () => void | Promise<void> = () => {};
  export let onSetModel: (provider: string, id: string) => void | Promise<void> = () => {};
  export let onSetAutoModel: () => void | Promise<void> = () => {};
  export let onRefresh: () => void | Promise<void> = () => {};
  export let onClose: () => void = () => {};

  let cwd = "";
  let modelSearch = "";
  let showModels = false;

  $: cwd = project?.cwd ?? "";
  $: query = modelSearch.trim().toLowerCase();
  $: filteredModels = query
    ? models.filter((model) =>
        `${model.provider}/${model.id}`.toLowerCase().includes(query),
      )
    : models;
  $: visibleModels = filteredModels.slice(0, 80);
  $: currentModelLabel = project?.model || "—";
  $: contextPct = Math.max(0, Math.min(100, project?.session_context_pct ?? 0));

  function isCurrentModel(model: ModelEntry): boolean {
    return project?.model === model.id || project?.model === `${model.provider}/${model.id}`;
  }

  function formatNumber(value: number): string {
    return new Intl.NumberFormat("pt-PT").format(value || 0);
  }

  function formatCost(value: number): string {
    return `$${(value || 0).toFixed(4)}`;
  }

  async function applyCwd() {
    const path = cwd.trim();
    if (!path) return;
    await onSetCwd(path);
  }

  async function pickFolder() {
    const selected = await open({
      directory: true,
      multiple: false,
      title: "Escolher diretório do projeto",
    });
    if (typeof selected === "string") {
      cwd = selected;
    }
  }

  async function chooseModel(model: ModelEntry) {
    await onSetModel(model.provider, model.id);
    showModels = false;
    modelSearch = "";
  }

  async function chooseAuto() {
    await onSetAutoModel();
    showModels = false;
    modelSearch = "";
  }
</script>

<div class="project-panel">
  <header class="panel-header">
    <div>
      <h2>Projeto</h2>
      <p>Binding local desta sessão</p>
    </div>
    <button class="icon-button" title="Fechar" on:click={onClose}>×</button>
  </header>

  <div class="panel-body">
    <section>
      <label for="cwd">Diretório de trabalho</label>
      <div class="input-row">
        <input
          id="cwd"
          bind:value={cwd}
          placeholder="/caminho/do/projeto"
          spellcheck="false"
          autocomplete="off"
          disabled={running}
        />
        <button class="primary small" on:click={applyCwd} disabled={running || !cwd.trim()}>
          Aplicar
        </button>
      </div>
      <div class="inline-actions">
        <button class="ghost small" on:click={pickFolder} disabled={running}>
          Escolher…
        </button>
        <button class="ghost small" on:click={onClearCwd} disabled={running || !project?.cwd}>
          Limpar projeto
        </button>
        <button class="ghost small" on:click={onRefresh} disabled={loading}>
          {loading ? "A atualizar…" : "Atualizar"}
        </button>
      </div>
      {#if !project?.cwd}
        <p class="hint">
          Sem projeto a sessão fica em <strong>chat mode</strong> e as ferramentas de
          filesystem ficam desabilitadas.
        </p>
      {/if}
    </section>

    <section>
      <div class="section-title">
        <span>Modelo</span>
        <span class="muted">{currentModelLabel}</span>
      </div>
      <div class="input-row">
        <button class="ghost" on:click={() => (showModels = !showModels)} disabled={running}>
          {showModels ? "Fechar catálogo" : "Escolher modelo"}
        </button>
        <button class="ghost" on:click={chooseAuto} disabled={running}>PI default</button>
      </div>

      {#if showModels}
        <input
          class="search"
          bind:value={modelSearch}
          placeholder="Filtrar por provider ou modelo…"
          spellcheck="false"
          autocomplete="off"
        />
        <div class="model-list">
          {#if modelsLoading}
            <p class="hint model-loading">A carregar catálogo de modelos…</p>
          {/if}
          {#each visibleModels as model (model.provider + "/" + model.id)}
            <button
              class="model-row"
              class:current={isCurrentModel(model)}
              on:click={() => chooseModel(model)}
              disabled={running}
            >
              <span class="model-provider">{model.provider}</span>
              <span class="model-id">{model.id}</span>
            </button>
          {/each}
          {#if visibleModels.length === 0 && !modelsLoading}
            <p class="hint">Nenhum modelo encontrado.</p>
          {/if}
          {#if filteredModels.length > visibleModels.length}
            <p class="hint">… e mais {filteredModels.length - visibleModels.length} modelos.</p>
          {/if}
        </div>
      {/if}
    </section>

    <section>
      <div class="section-title">
        <span>Estado</span>
        {#if project}
          <span class="status-pill" class:online={project.bridge_status === "online"}>
            {project.bridge_status || "—"}
          </span>
        {/if}
      </div>

      {#if project}
        <dl class="facts">
          <div><dt>Agente</dt><dd>{project.active_agent || "—"}</dd></div>
          <div><dt>Binding</dt><dd>{project.binding_source || "—"}</dd></div>
          {#if project.binding_from}
            <div><dt>Herdado de</dt><dd>{project.binding_from}</dd></div>
          {/if}
          <div><dt>Checkpoint</dt><dd>{project.checkpoint_layer || "—"}</dd></div>
        </dl>

        <div class="context-block">
          <div class="context-line">
            <span>Contexto</span>
            <span>{contextPct.toFixed(1)}%</span>
          </div>
          <div class="progress"><div class="progress-fill" style={`width: ${contextPct}%`}></div></div>
          <div class="context-line muted">
            <span>{formatNumber(project.session_context_tokens)} tokens</span>
            <span>janela {formatNumber(project.session_context_window)}</span>
          </div>
        </div>

        <dl class="facts">
          <div><dt>Input</dt><dd>{formatNumber(project.session_input_tokens)}</dd></div>
          <div><dt>Output</dt><dd>{formatNumber(project.session_output_tokens)}</dd></div>
          <div><dt>Custo</dt><dd>{formatCost(project.session_cost_usd)}</dd></div>
        </dl>

        {#if project.latest_run}
          <div class="latest-run">
            <div class="section-title">
              <span>Último run</span>
              <span class="status-pill">{project.latest_run.status}</span>
            </div>
            <p class="hint">
              {project.latest_run.agent_name || "—"}
              {#if project.latest_run.duration_ms}· {project.latest_run.duration_ms}ms{/if}
              {#if project.latest_run.checkpoint}· {project.latest_run.checkpoint}{/if}
            </p>
          </div>
        {/if}

        {#if project.memory_layers.length > 0}
          <div class="memory-list">
            <div class="section-title"><span>Memória</span></div>
            {#each project.memory_layers as layer (layer.name)}
              <div class="memory-row">
                <span>{layer.name}</span>
                <span class="muted">{layer.exists ? `${layer.file_count} ficheiros` : "ausente"}</span>
              </div>
            {/each}
          </div>
        {/if}
      {:else}
        <p class="hint">Sem dados de estado. Atualize ou verifique o daemon.</p>
      {/if}
    </section>
  </div>
</div>
