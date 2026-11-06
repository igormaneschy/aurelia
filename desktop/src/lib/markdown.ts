/**
 * Markdown rendering, configured once.
 *
 * `marked.use(markedHighlight(...))` must NOT run inside a component: the
 * marked instance is global and `use()` chains the `walkTokens` of the
 * previous registration (each call runs the new walkTokens *and* the old
 * one). Calling it once per component instance — once per rendered message
 * — therefore highlighted every code block N times, each pass re-processing
 * the HTML of the previous one. The output grew geometrically (a 6 KB
 * message produced 6.5 MB of HTML) and froze the window for about a second
 * per message.
 *
 * This module is imported once per application, so the extension is
 * registered exactly once and every parse highlights each block a single
 * time.
 */
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

/**
 * Renders markdown to sanitized HTML.
 *
 * The output size is bounded: a runaway highlight (or a pathological
 * document) must not be able to produce megabytes of HTML for a few
 * kilobytes of text, which is what froze the transcript.
 */
const MAX_RENDERED_CHARS = 400_000;

export function renderMarkdown(text: string): string {
  const html = marked.parse(text) as string;
  if (html.length > MAX_RENDERED_CHARS) {
    // Fall back to the plain text: the rendered form is not trustworthy at
    // this size, and the DOM cost would be far worse than plain text.
    return `<pre class="markdown-plain">${DOMPurify.sanitize(text)}</pre>`;
  }
  return DOMPurify.sanitize(html);
}
