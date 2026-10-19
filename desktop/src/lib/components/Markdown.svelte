<script lang="ts">
  import DOMPurify from "dompurify";
  import hljs from "highlight.js/lib/core";
  import bash from "highlight.js/lib/languages/bash";
  import css from "highlight.js/lib/languages/css";
  import diff from "highlight.js/lib/languages/diff";
  import go from "highlight.js/lib/languages/go";
  import javascript from "highlight.js/lib/languages/javascript";
  import json from "highlight.js/lib/languages/json";
  import markdown from "highlight.js/lib/languages/markdown";
  import plaintext from "highlight.js/lib/languages/plaintext";
  import python from "highlight.js/lib/languages/python";
  import rust from "highlight.js/lib/languages/rust";
  import sql from "highlight.js/lib/languages/sql";
  import typescript from "highlight.js/lib/languages/typescript";
  import xml from "highlight.js/lib/languages/xml";
  import yaml from "highlight.js/lib/languages/yaml";
  import { marked } from "marked";
  import { markedHighlight } from "marked-highlight";
  import { onDestroy } from "svelte";
  import "highlight.js/styles/github-dark.css";

  hljs.registerLanguage("bash", bash);
  hljs.registerLanguage("css", css);
  hljs.registerLanguage("diff", diff);
  hljs.registerLanguage("go", go);
  hljs.registerLanguage("javascript", javascript);
  hljs.registerLanguage("json", json);
  hljs.registerLanguage("markdown", markdown);
  hljs.registerLanguage("plaintext", plaintext);
  hljs.registerLanguage("python", python);
  hljs.registerLanguage("rust", rust);
  hljs.registerLanguage("sql", sql);
  hljs.registerLanguage("typescript", typescript);
  hljs.registerLanguage("xml", xml);
  hljs.registerLanguage("yaml", yaml);

  marked.use(
    markedHighlight({
      emptyLangClass: "hljs",
      langPrefix: "hljs language-",
      highlight(code, language) {
        const resolved = language && hljs.getLanguage(language) ? language : "plaintext";
        return hljs.highlight(code, { language: resolved }).value;
      },
    }),
  );
  marked.setOptions({ gfm: true, breaks: true });

  export let text = "";
  /**
   * While the message is still streaming the text is incomplete: parsing it
   * as markdown renders half-open constructs literally (`**` with no closing
   * pair) and re-parses the whole growing text on every chunk, which is
   * O(n²) work on the main thread. During streaming the raw text is shown
   * instead; the markdown is rendered once, when the message is complete.
   */
  export let streaming = false;

  // Non-reactive bookkeeping: a plain object is not proxied in legacy mode, so
  // mutating it does not schedule another update cycle.
  const state = {
    rendered: "",
    pending: text,
    lastRenderAt: 0,
    timer: undefined as ReturnType<typeof setTimeout> | undefined,
  };

  let html = "";

  function render(value: string): string {
    return DOMPurify.sanitize(marked.parse(value) as string);
  }

  function flush() {
    state.timer = undefined;
    if (state.pending === state.rendered) return;
    state.rendered = state.pending;
    html = render(state.rendered);
    state.lastRenderAt = Date.now();
  }

  function schedule(value: string) {
    state.pending = value;
    if (state.pending === state.rendered) return;

    const elapsed = Date.now() - state.lastRenderAt;
    if (elapsed >= RENDER_INTERVAL_MS) {
      if (state.timer) {
        clearTimeout(state.timer);
        state.timer = undefined;
      }
      flush();
      return;
    }

    // Too soon after the last render: coalesce the remaining work.
    if (state.timer) return;
    state.timer = setTimeout(flush, RENDER_INTERVAL_MS - elapsed);
  }

  const RENDER_INTERVAL_MS = 100;

  $: if (streaming) {
    // Streaming: show the raw text, no markdown parsing.
    if (state.timer) {
      clearTimeout(state.timer);
      state.timer = undefined;
    }
    state.rendered = text;
    state.pending = text;
    html = "";
  } else {
    schedule(text);
  }

  onDestroy(() => {
    if (state.timer) clearTimeout(state.timer);
  });
</script>

{#if streaming}
  <div class="markdown markdown-raw">{text}</div>
{:else}
  <div class="markdown">{@html html}</div>
{/if}
