# Global singletons: configure them once, not per component

**Date**: 2026-10-20
**Change**: desktop-app-render-unblock
**Category**: anti-pattern

## What happened

Switching to a session with a longer history froze the desktop window. The
transcript was building megabytes of HTML out of a few kilobytes of text: one
5,955-character message produced **6,574,166** characters of HTML, costing
~440 ms in `marked` plus ~560 ms in `DOMPurify` — and it got worse with every
message rendered.

The cause was a **global singleton configured per component instance**:
`marked.use(markedHighlight(...))` was called inside the Markdown component,
so it ran once per rendered message. The `marked` instance is global, and
`use()` *chains* the `walkTokens` of the previous registration — each call
runs the new walkTokens **and** the old one. Every code block was therefore
highlighted once per registered component, each pass re-processing the HTML
of the previous one, so the output grew geometrically.

## How to avoid

- Configure global libraries (marked, highlight.js, i18n, formatters) in a
  **module** that the component imports, so the configuration runs once per
  application, not once per instance.
- Read the API contract before relying on it: `marked.use()` chaining
  `walkTokens` is invisible until the output explodes.
- Add a **size guard** on the output of a render pipeline (cap the HTML and
  fall back to plain text). A bound turns a freeze into a degraded view.
- Measure the output size, not just the time: the time looked like "slow
  rendering" (95 ms in Node), but the output was 1000x the input — that is
  the signal that something is compounding.

## Tags

#lesson #change-desktop-app-render-unblock #anti-pattern #markdown #marked #performance
