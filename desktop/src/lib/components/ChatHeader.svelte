<script lang="ts">
  import type { DaemonStatus, ProjectState, SessionInfo } from "../types";

  export let session: SessionInfo | null = null;
  export let project: ProjectState | null = null;
  export let daemon: DaemonStatus | null = null;
  export let running = false;
  export let runStatus = "";

  $: title = !session
    ? "Aurelia"
    : session.chat_id === -9000001
      ? "Aurelia / DM"
      : `Aurelia / ${session.name}`;

  $: chatMode = !project?.cwd;
  $: bridgeOnline = daemon?.online === true && project?.bridge_status !== "offline";
</script>

<header class="chat-header">
  <div class="title-row">
    <div>
      <h1>{title}</h1>
      <div class="chips">
        <span class="chip">{project?.model || "modelo —"}</span>
        <span class="chip" class:online={bridgeOnline} class:offline={!bridgeOnline}>
          <span class="dot"></span>
          bridge {bridgeOnline ? "online" : "offline"}
        </span>
        {#if chatMode}
          <span class="chip chat-mode">chat mode</span>
        {/if}
      </div>
    </div>

    {#if running}
      <div class="running-chip">
        <span class="pulse"></span>
        {runStatus || "A responder…"}
      </div>
    {/if}
  </div>
</header>
