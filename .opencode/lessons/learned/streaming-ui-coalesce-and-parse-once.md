# Streaming UIs: coalesce events and parse markdown once

**Date**: 2026-10-20
**Change**: desktop-app-render-unblock
**Category**: anti-pattern

## What happened

In the desktop client (Tauri + Svelte 5), the window froze while a response
streamed in and the text painted sluggishly. Two O(n²) costs in the response
length, both invisible in a short answer:

- The daemon emits one `stream_chunk` per delta. Every chunk triggered a
  transcript re-render, and the growing message was re-parsed as markdown
  (marked + highlight.js + DOMPurify) over its **whole** length on every
  chunk.
- The autoscroll read `scrollHeight` on every update, forcing a synchronous
  layout per chunk.

A related defect made the transcript look broken: while a message was
incomplete, parsing it as markdown rendered half-open constructs literally
(`**` with no closing pair) and the text appeared cut mid-sentence.

## How to avoid

- Coalesce stream events on both sides of the bridge (Rust: ~40ms, frontend:
  ~60ms) and always flush the pending text on the terminal events, so nothing
  is lost. Painting fewer, larger updates is what makes a stream feel fluid.
- While a message is still streaming, show raw text; parse the markdown once
  when it is complete. Half-parsed markdown is worse than no markdown.
- In an autoscroll, only read layout metrics when the content actually grew,
  and only write the "stick to bottom" flag when it changes: assigning the
  same value still invalidates the component in Svelte 5.
- Measure before tuning: `ps` on the WebContent process during a stream shows
  whether the main thread is saturated (100% vs 2-4%).

## Related trap: the bridge bundle is a copy

`internal/bridge/bundle.ts` is a **copy** of `bridge/index.ts` embedded in the
Go binary; the daemon builds the bridge from it and caches the result in
`~/.aurelia/bridge/bundle.js`. Changing `bridge/index.ts` without running
`make bridge` leaves the daemon running the old code — the fix appears to do
nothing. Always run `make bridge` (it syncs the copy and rebuilds the bundle)
before deploying a bridge change.

## Tags

#lesson #change-desktop-app-render-unblock #anti-pattern #streaming #svelte #performance #bridge
