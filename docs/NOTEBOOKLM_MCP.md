# NotebookLM MCP — Gemini Notebook integration

Status: **installed on the NAS, read-only MCP, authentication pending**. The
server is `notebooklm-py` 0.8.6 in a dedicated virtualenv; the PI MCP adapter
will start it lazily when the assistant calls a NotebookLM tool.

NotebookLM was rebranded **Gemini Notebook** in July 2026. The product and the
underlying service are the same; this integration keeps the `notebooklm` name.

## Architecture

```text
Aurelia daemon (NAS)
  └─ bridge / PI SDK
       └─ pi-mcp-adapter
            └─ notebooklm-mcp (stdio, read-only tool subset)
                 └─ ~/.notebooklm/profiles/default/
                      └─ master_token.json (dedicated account)
```

- **MCP, not a custom plugin.** The official PI MCP adapter exposes the `mcp`
  proxy tool; the model searches and calls the NotebookLM tools on demand.
- **stdio transport.** No local HTTP port is opened; the adapter starts the
  server only when a tool is used and stops it after the idle timeout.
- **Read-only by default.** The MCP entry includes only listing, reading,
  asking, status, and suggestion tools. Mutating/destructive tools are not
  exposed until explicitly enabled.

## 1. Dedicated Google account

Do **not** use the personal Google account for the master token. The token is a
durable full-account credential.

1. Create a dedicated Google account for automation.
2. From the personal NotebookLM account, share the notebooks the assistant may
   read with the dedicated account as **viewer**.
3. Sign in to NotebookLM with the dedicated account once to accept the shares
   and confirm the notebooks appear.

## 2. One-time master token on a workstation

Run this on a machine with a browser (for example the Mac), not on the NAS:

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
uv tool install "notebooklm-py[headless,browser]"

notebooklm login --master-token --account <dedicated-account>
notebooklm auth check --test --json
```

The login writes:

```text
~/.notebooklm/profiles/default/master_token.json
~/.notebooklm/profiles/default/storage_state.json
```

The master token is the durable credential; `storage_state.json` is the
short-lived cookie snapshot. Protect both (`0600`) and never commit them.

## 3. NAS installation

The NAS user needs `uv` (user-space install; no root):

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

uv venv --python /usr/bin/python3 "$HOME/.aurelia/venvs/notebooklm"
uv pip install \
  --python "$HOME/.aurelia/venvs/notebooklm/bin/python" \
  "notebooklm-py[mcp,headless]"
```

Pin the tested version in operations notes:

```text
notebooklm-py 0.8.6
```

Copy the master token from the workstation:

```bash
mkdir -p ~/.notebooklm/profiles/default
chmod 700 ~/.notebooklm ~/.notebooklm/profiles ~/.notebooklm/profiles/default
scp <mac-user>@<mac-host>:~/.notebooklm/profiles/default/master_token.json \
  ~/.notebooklm/profiles/default/master_token.json
chmod 600 ~/.notebooklm/profiles/default/master_token.json
```

Bootstrap the Web session from the token and verify it against Google:

```bash
~/.aurelia/venvs/notebooklm/bin/notebooklm login --master-token-refresh
~/.aurelia/venvs/notebooklm/bin/notebooklm auth check --test --json
```

Expected: `"status": "ok"` and `"checks": { "token_fetch": true, ... }`.
The server re-mints expired cookies from the master token; no browser is
needed on the NAS.

## 4. PI MCP configuration

Add to `~/.aurelia/pi-agent/mcp.json`:

```jsonc
{
  "mcpServers": {
    "notebooklm": {
      "command": "/home/<nas-user>/.aurelia/venvs/notebooklm/bin/notebooklm-mcp",
      "args": ["--transport", "stdio", "--profile", "default"],
      "cwd": "/home/<nas-user>",
      "lifecycle": "lazy",
      "timeout": 60,
      "includeTools": [
        "notebooklm_notebook_list",
        "notebooklm_notebook_describe",
        "notebooklm_source_list",
        "notebooklm_source_read",
        "notebooklm_source_wait",
        "notebooklm_chat_ask",
        "notebooklm_chat_status",
        "notebooklm_studio_list",
        "notebooklm_studio_status",
        "notebooklm_suggest_prompts",
        "notebooklm_server_info"
      ],
      "description": "Gemini Notebook (NotebookLM): read-only cited Q&A over notebooks and sources."
    }
  }
}
```

Tool names are prefixed by the adapter with the server name (`notebooklm_`).
The `mcp` proxy tool is already part of the PI extension allowlist; no Go
allowlist change is needed for read-only use.

## 5. Restart and smoke test

```bash
systemctl --user restart aurelia.service
journalctl --user -u aurelia.service -n 50 --no-pager
```

Then, from a chat:

```text
Utiliza o NotebookLM: lista os notebooks disponíveis.
```

The first MCP call may need a connect if metadata is stale:

```text
mcp({ connect: "notebooklm" })
mcp({ search: "notebook" })
mcp({ tool: "notebooklm_notebook_list", args: {} })
```

A successful call returns the shared notebooks. If it returns `AUTH`, run
`notebooklm auth check --test --json` on the NAS and inspect the profile.

## 6. Security boundaries

- The master token is **full-account and durable**; use it only with the
  dedicated account. Revoke the device/session in the Google account if it
  leaks; a password change does not revoke it.
- `~/.notebooklm/**` is denied to the agent's Read/Bash/Grep tools and redacted
  from audit output. The MCP server process itself still reads the profile.
- Do not put the token in `app.json`, the repo, logs, or prompts.
- The MCP entry is read-only. To enable writes/generation later, add only the
  specific tools (`source_add`, `studio_generate`, …) and keep destructive
  tools excluded or behind an approval policy.
- The server runs with `stdio`; no network listener is opened. If an HTTP
  transport is ever used, bind to loopback and require a bearer token.

## 7. Operations

| Symptom | Check |
|---|---|
| `AUTH` on every call | `notebooklm auth check --test --json`; re-copy `master_token.json` if needed |
| Server does not start | `~/.aurelia/venvs/notebooklm/bin/notebooklm-mcp --help`; check the `command` path |
| Tools missing | Confirm `includeTools` names match `notebooklm_*`; restart the daemon and reconnect the MCP server |
| Stale metadata | `mcp({ connect: "notebooklm" })`, then search again |
| Quota/rate limit | Wait for the retry window; NotebookLM quotas apply to the dedicated account |

Update `notebooklm-py` deliberately (MCP tool shapes are experimental):

```bash
uv pip install --python ~/.aurelia/venvs/notebooklm/bin/python \
  "notebooklm-py[mcp,headless]==<version>"
systemctl --user restart aurelia.service
```

## Alternatives

- **`roomi-fields/notebooklm-mcp`** — active MCP + REST server with a browser
  fallback and Docker deployment. Heavier (Node 22, Docker/Chromium) and useful
  if the Python RPC path breaks or if a REST endpoint is required.
- **`PleasePrompto/notebooklm-mcp`** — archived in September 2026; not
  recommended for new deployments.

There is no public official NotebookLM API for personal accounts; both active
options use undocumented Google endpoints and may break when Google changes
them.
