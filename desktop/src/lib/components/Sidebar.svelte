<script lang="ts">
  import { DM_CHAT_ID, type DaemonStatus, type ProjectState, type SessionInfo } from "../types";

  export let sessions: SessionInfo[] = [];
  export let activeSession: SessionInfo | null = null;
  export let daemon: DaemonStatus | null = null;
  export let project: ProjectState | null = null;
  export let running = false;

  export let onSelect: (session: SessionInfo) => void = () => {};
  export let onCreate: (name: string) => void | Promise<void> = () => {};
  export let onRefresh: () => void = () => {};
  export let onShowProject: () => void = () => {};
  export let onShowStatus: () => void = () => {};
  export let onShowProfiles: () => void = () => {};
  export let onShowHelp: () => void = () => {};
  export let onReset: () => void | Promise<void> = () => {};

  let creating = false;
  let newName = "";

  function basename(path: string): string {
    return path.split("/").filter(Boolean).pop() || path;
  }

  function label(session: SessionInfo): string {
    return session.chat_id === DM_CHAT_ID ? "DM" : session.name;
  }

  async function submitCreate() {
    const name = newName.trim();
    if (!name) return;
    creating = false;
    newName = "";
    await onCreate(name);
  }

  $: projectLabel = project?.cwd ? basename(project.cwd) : "Sem projeto";
  $: bridgeLabel = daemon?.online ? project?.bridge_status || "online" : "offline";
</script>

<aside class="sidebar">
  <div class="brand">
    <div class="brand-mark">A</div>
    <div>
      <div class="brand-name">Aurelia</div>
      <div class="brand-sub">local desktop</div>
    </div>
  </div>

  <div class="section-header">
    <span>Sessões</span>
    <button class="icon-button" title="Nova sessão" on:click={() => (creating = true)} disabled={running}>
      +
    </button>
  </div>

  {#if creating}
    <form class="new-session" on:submit|preventDefault={submitCreate}>
      <input
        bind:value={newName}
        placeholder="nome da sessão"
        maxlength="64"
        autocomplete="off"
        spellcheck="false"
      />
      <button class="primary small" type="submit" disabled={!newName.trim()}>Criar</button>
      <button class="ghost small" type="button" on:click={() => (creating = false)}>Cancelar</button>
    </form>
  {/if}

  <nav class="session-list" aria-label="Sessões">
    {#each sessions as session (session.chat_id)}
      <button
        class="session-row"
        class:active={session.chat_id === activeSession?.chat_id}
        disabled={running && session.chat_id !== activeSession?.chat_id}
        title={label(session)}
        on:click={() => onSelect(session)}
      >
        <span class="session-dot"></span>
        <span class="session-name">{label(session)}</span>
        {#if session.message_count > 0}
          <span class="session-count">{session.message_count}</span>
        {/if}
      </button>
    {/each}
  </nav>

  <div class="section-header context-header">
    <span>Contexto</span>
  </div>

  <div class="context">
    <button class="context-row" on:click={onShowProject}>
      <span class="context-icon">📂</span>
      <span class="context-text" title={project?.cwd || "Nenhum projeto definido"}>
        {projectLabel}
      </span>
    </button>
    <div class="context-row static" title={project?.model || ""}>
      <span class="context-icon">🤖</span>
      <span class="context-text">{project?.model || "—"}</span>
    </div>
    <div class="context-row static" title={daemon?.socket_path || ""}>
      <span class="context-icon">●</span>
      <span class="context-text" class:online={daemon?.online} class:offline={!daemon?.online}>
        {bridgeLabel}
      </span>
    </div>
  </div>

  <div class="sidebar-actions">
    <button on:click={onShowProject}>Projeto</button>
    <button on:click={onShowStatus}>Status</button>
    <button on:click={onShowProfiles}>Perfis</button>
    <button on:click={onShowHelp}>Ajuda</button>
    <button on:click={onReset} disabled={running}>Reset</button>
    <button on:click={onRefresh}>Atualizar</button>
  </div>

  <div class="sidebar-footer">
    <span class:online={daemon?.online} class:offline={!daemon?.online}>
      {daemon?.online
        ? `daemon online${daemon.latency_ms != null ? ` · ${daemon.latency_ms}ms` : ""}`
        : "daemon offline"}
    </span>
  </div>
</aside>
