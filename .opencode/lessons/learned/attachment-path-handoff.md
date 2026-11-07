# Attachment delivery is not content extraction

**Date**: 2026-11-07
**Change**: telegram-document-uploads
**Category**: pattern

## What happened

Telegram rejected spreadsheets and supplied only a placeholder for accepted PDFs.
The PI agent nevertheless processed copies it found in Downloads, masking the
broken attachment handoff rather than proving delivery worked.

## How to avoid

Persist the original bytes and supply the exact saved path and escaped metadata.
Test the saved bytes and path handoff independently of agent-selected converters;
never claim extraction occurred when only metadata was provided. Keep filenames
out of command/control classification: `status.xlsx` is input, not a status request.

## Tags

#lesson #change-telegram-document-uploads #pattern #attachments #pi #telegram
