/**
 * Benchmark: custo de parse de markdown (marked + highlight.js) do histórico
 * real devolvido pelo daemon — o mesmo caminho que o app desktop executa ao
 * trocar de sessão (cada mensagem vira um componente Markdown).
 */
import { marked } from "marked";
import { markedHighlight } from "marked-highlight";
import hljs from "highlight.js/lib/core";
import plaintext from "highlight.js/lib/languages/plaintext";
import bash from "highlight.js/lib/languages/bash";
import go from "highlight.js/lib/languages/go";
import json from "highlight.js/lib/languages/json";
import rust from "highlight.js/lib/languages/rust";
import typescript from "highlight.js/lib/languages/typescript";

for (const [name, lang] of Object.entries({ bash, go, json, rust, typescript, plaintext })) {
  hljs.registerLanguage(name, lang);
}

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

const history = JSON.parse(process.argv[2]);
let totalChars = 0;
let totalMs = 0;
let worst = { ms: 0, len: 0 };

for (const msg of history) {
  const started = performance.now();
  marked.parse(msg.text);
  const ms = performance.now() - started;
  totalMs += ms;
  totalChars += msg.text.length;
  if (ms > worst.ms) worst = { ms, len: msg.text.length };
}

console.log(`mensagens=${history.length} chars=${totalChars} parse_total=${totalMs.toFixed(1)}ms pior=${worst.ms.toFixed(1)}ms (${worst.len} chars)`);
