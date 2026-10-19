<script lang="ts">
  import { tick } from "svelte";
  import { open } from "@tauri-apps/plugin-dialog";
  import {
    addPendingPaths,
    notify,
    pendingAttachments,
    removePending,
  } from "../stores";

  export let running = false;
  export let disabled = false;
  export let onSend: (text: string) => Promise<boolean> = async () => false;
  export let onCancel: () => void = () => {};

  let text = "";
  let textarea: HTMLTextAreaElement;

  function resize() {
    if (!textarea) return;
    textarea.style.height = "auto";
    textarea.style.height = `${Math.min(textarea.scrollHeight, 180)}px`;
  }

  async function submit() {
    if (running || disabled) return;
    const value = text.trim();
    if (!value) return;

    const accepted = await onSend(value);
    if (!accepted) return;

    text = "";
    await tick();
    resize();
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
    event.preventDefault();
    sendNow();
  }

  // `submit()` can reject (IPC failure, runtime error). Without this the
  // rejection is swallowed by `void` and the composer looks frozen.
  function sendNow() {
    void submit().catch((error) => {
      notify("error", error instanceof Error ? error.message : String(error));
    });
  }

  async function attach() {
    const selected = await open({
      multiple: true,
      directory: false,
      title: "Anexar arquivos",
    });
    if (!selected) return;
    addPendingPaths(Array.isArray(selected) ? selected : [selected]);
  }
</script>

<div class="composer">
  {#if $pendingAttachments.length > 0}
    <div class="attachment-list">
      {#each $pendingAttachments as attachment, index (attachment.path)}
        <div class="attachment-chip" title={attachment.path}>
          <span class="attachment-icon">{attachment.kind === "image" ? "🖼" : "📎"}</span>
          <span class="attachment-name">{attachment.name}</span>
          <button
            class="attachment-remove"
            title="Remover anexo"
            on:click={() => removePending(index)}
          >×</button>
        </div>
      {/each}
    </div>
  {/if}

  <div class="composer-box">
    <textarea
      bind:this={textarea}
      bind:value={text}
      rows="1"
      placeholder={disabled ? "Daemon offline — inicie o serviço para conversar" : "Mensagem para a Aurelia…"}
      spellcheck="true"
      on:input={resize}
      on:keydown={onKeydown}
    ></textarea>

    <div class="composer-actions">
      <button class="ghost" title="Anexar imagem ou documento" on:click={attach}>Anexar</button>
      {#if running}
        <button class="danger" on:click={onCancel}>Cancelar</button>
      {:else}
        <button class="primary" on:click={sendNow} disabled={disabled || !text.trim()}>
          Enviar
        </button>
      {/if}
    </div>
  </div>

  <div class="composer-hint">
    <span>Enter envia · Shift+Enter nova linha</span>
    <span>{$pendingAttachments.length}/10 anexos</span>
  </div>
</div>
