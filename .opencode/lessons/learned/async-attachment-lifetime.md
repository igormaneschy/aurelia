# Uploaded files must outlive the transport handler

**Date**: 2026-11-07
**Change**: telegram-document-uploads
**Category**: anti-pattern

## What happened

The Telegram document path deleted its temporary download when the handler
returned. That lifetime cannot support asynchronous agent execution, retries,
or later turns that reference the same attachment.

## How to avoid

Keep successful uploads in the bound project's uploads directory. Delete only
partial or rejected writes automatically, and test that the exact file remains
readable after the download context is cancelled and the preparation returns.

## Tags

#lesson #change-telegram-document-uploads #anti-pattern #attachments #async #lifecycle
