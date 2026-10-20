<script lang="ts">
  import { afterUpdate } from "svelte";
  import type { ChatMessage } from "../types";
  import Markdown from "./Markdown.svelte";

  export let messages: ChatMessage[] = [];
  export let runStatus = "";
  export let running = false;
  export let loading = false;

  let viewport: HTMLDivElement;
  let stickToBottom = true;

  $: if (messages.length === 0) stickToBottom = true;

  afterUpdate(() => {
    // Autoscroll must run synchronously inside the same update cycle.
    // An `await tick()` here re-enters the update pipeline after the flush
    // completes, and writing the bound element re-invalidates it — the two
    // combined kept the microtask queue permanently drained (WebContent at
    // ~100% CPU and the UI never painted).
    //
    // The element is copied into a local before the write on purpose:
    // `bind:this` creates a reactive source, and assigning a property on it
    // compiles to `mutate(source, …)`, which re-schedules the component —
    // afterUpdate then ran again, mutated again, and the loop never ended
    // (the window froze on "A carregar histórico…"). Writing through the
    // local copy touches the DOM without touching the reactive graph.
    //
    // `stickToBottom` is the user's intent: it is false only after the user
    // scrolls up. It must not be derived from the current distance — after a
    // session switch the transcript is empty and then fills up, so the
    // distance is large and the view stayed at the top of the conversation.
    const node = viewport;
    if (!stickToBottom || !node) return;
    node.scrollTop = node.scrollHeight;
  });

  function onScroll() {
    if (!viewport) return;
    const atBottom =
      viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 80;
    // Only write when the value actually changes: assigning the same boolean
    // still invalidates the component and re-runs the update cycle.
    if (atBottom !== stickToBottom) {
      stickToBottom = atBottom;
    }
  }

  function formatTime(timestamp?: string): string {
    if (!timestamp) return "";
    const date = new Date(timestamp);
    if (Number.isNaN(date.getTime())) return "";
    return date.toLocaleTimeString("pt-PT", {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
</script>

<div class="transcript" bind:this={viewport} on:scroll={onScroll}>
  {#if loading}
    <div class="state-message">A carregar histórico…</div>
  {:else if messages.length === 0}
    <div class="empty-state">
      <div class="empty-mark">A</div>
      <h2>Aurelia</h2>
      <p>Conversa local com o daemon. As sessões são compartilhadas com a TUI.</p>
      <ul>
        <li><strong>Enter</strong> envia · <strong>Shift+Enter</strong> nova linha</li>
        <li>Anexe imagens e documentos pelo clipe</li>
        <li>Use <strong>Projeto</strong> para definir o <code>/cwd</code></li>
      </ul>
    </div>
  {:else}
    {#each messages as message (message.id)}
      {#if message.role === "system" || message.role === "error"}
        <div class="system-row {message.role}">{message.text}</div>
      {:else}
        <article class="message {message.role}">
          <div class="message-meta">
            <span>{message.role === "user" ? "Você" : "Aurelia"}</span>
            {#if message.timestamp}
              <span>{formatTime(message.timestamp)}</span>
            {/if}
          </div>
          <div class="bubble">
            {#if message.role === "assistant"}
              <Markdown text={message.text} streaming={message.streaming} />
              {#if message.streaming}<span class="cursor"></span>{/if}
            {:else}
              <p>{message.text}</p>
            {/if}
          </div>
        </article>
      {/if}
    {/each}
  {/if}

  {#if running && runStatus}
    <div class="run-status">
      <span class="pulse"></span>
      {runStatus}
    </div>
  {/if}
</div>
