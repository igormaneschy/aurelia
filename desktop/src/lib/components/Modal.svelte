<script lang="ts">
  import Markdown from "./Markdown.svelte";

  export let title = "";
  export let body = "";
  export let onClose: () => void = () => {};
</script>

<div class="modal-backdrop" role="presentation" on:click|self={onClose}>
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <header class="panel-header">
      <h2>{title}</h2>
      <button class="icon-button" title="Fechar" on:click={onClose}>×</button>
    </header>
    <div class="modal-body">
      <Markdown text={body} />
    </div>
  </div>
</div>
