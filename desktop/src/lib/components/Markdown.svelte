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

  $: html = DOMPurify.sanitize(marked.parse(text) as string);
</script>

<div class="markdown">{@html html}</div>
