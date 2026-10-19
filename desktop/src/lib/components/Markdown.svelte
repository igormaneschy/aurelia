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

  // Rendering markdown is expensive (marked + highlight.js + DOMPurify).
  // While a message streams in, `text` changes on every chunk, so parsing on
  // every change re-does the whole work over the whole (growing) text: O(n²)
  // work that blocks the main thread, freezing the window and making the
  // response render sluggishly. Throttle the parsing to a fixed interval.
  const RENDER_INTERVAL_MS = 100;

  function parseMarkdown(value: string): string {
    return DOMPurify.sanitize(marked.parse(value) as string);
  }

  /**
   * The throttled-render bookkeeping lives in a closure on purpose.
   *
   * In Svelte 5 legacy mode any object assigned to a component variable
   * (object literal or class instance) becomes a reactive source, and every
   * property write then compiles to `mutate(source, …)` — which
   * re-schedules the component and re-runs the reactive statement that
   * called it, in an endless loop that froze the window on "A carregar
   * histórico…". Locals of a nested function are plain JavaScript variables
   * and stay outside the reactive graph.
   */
  function createRenderer(initial: string) {
    let rendered = "";
    let pending = initial;
    let lastRenderAt = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function flush(apply: (html: string) => void) {
      timer = undefined;
      if (pending === rendered) return;
      rendered = pending;
      apply(parseMarkdown(rendered));
      lastRenderAt = Date.now();
    }

    return {
      /** Renders `value` as markdown, throttled to one parse per interval. */
      schedule(value: string, apply: (html: string) => void) {
        pending = value;
        if (pending === rendered) return;

        const elapsed = Date.now() - lastRenderAt;
        if (elapsed >= RENDER_INTERVAL_MS) {
          if (timer) {
            clearTimeout(timer);
            timer = undefined;
          }
          flush(apply);
          return;
        }

        // Too soon after the last render: coalesce the remaining work.
        if (timer) return;
        timer = setTimeout(() => flush(apply), RENDER_INTERVAL_MS - elapsed);
      },
      /** Streaming state: the text is shown raw, so drop any pending parse. */
      reset(value: string) {
        if (timer) {
          clearTimeout(timer);
          timer = undefined;
        }
        rendered = value;
        pending = value;
      },
      dispose() {
        if (timer) clearTimeout(timer);
      },
    };
  }

  const renderer = createRenderer(text);
  let html = "";

  $: if (streaming) {
    renderer.reset(text);
  } else {
    renderer.schedule(text, (value) => {
      html = value;
    });
  }

  onDestroy(() => {
    renderer.dispose();
  });
</script>

{#if streaming}
  <div class="markdown markdown-raw">{text}</div>
{:else}
  <div class="markdown">{@html html}</div>
{/if}
