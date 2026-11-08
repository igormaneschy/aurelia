# NAS Personal Assistant — Operations Runbook

Status: **live (2026-11-07), release v0.53.0**. The NAS is the sole Telegram
consumer; the Mac runs only the desktop client connected over Tailscale; the
TUI client is disabled. v0.53.0 was merged (`d44b319`), tagged and deployed to
the NAS; the next release is TBD.

Sections 1–8 are the pre-cutover plan and validation history. Sections 9–18 are
the current operating state, desktop access, memory-sharing rules and daily
runbook. Private identifiers (Telegram IDs, hostnames, Tailscale addresses and
personal paths) are intentionally placeholders in this public repository; the
real values live in `~/.aurelia/config/app.json` on the NAS and
`~/.aurelia/nas-tunnel.env` on the Mac.

Related change: `openspec/changes/nas-personal-assistant/`.

## 1. Scope and safety boundaries

- Aurelia runs natively on the NAS as a personal assistant. Docker is not a
  prerequisite.
- The NAS hosts a dedicated personal ai-memory. The Mac ai-memory used for
  coding is not a fallback and is not part of this installation.
- The existing personal folder is `/home/<nas-user>` (same directory as
  `/volume2/@home/<nas-user>`). It is not recreated or moved.
- No source data is deleted. No coding wiki, PI sessions, caches, credentials,
  or absolute Mac paths are copied indiscriminately.
- The Mac remains the Telegram consumer until an explicitly approved cutover
  window. No second process may poll the production bot.
- A NAS reboot, firewall/Tailscale change, port exposure, release promotion, or
  production cutover requires separate approval.

> Historical note: sections 1–8 describe the pre-cutover plan. The Mac consumer
> was stopped on 2026-11-07; see sections 9, 12 and 13 for the live state.

## 2. Version matrix

Initial candidate baseline: branch `feature/nas-personal-assistant`, commit
`bbf64642345ebc2bfea28743b9948f1387828d32` (Aurelia v0.51.0). The v0.52.0
attachments feature entered the base and the v0.53.0 release was rebased on it.

| Component | Pinned version | Source / verification |
|---|---|---|
| Aurelia release | `d44b319` / v0.53.0 | `main` after PR #88; annotated tag `v0.53.0` |
| Initial candidate | `bbf6464` / v0.51.0 | historical baseline |
| Go toolchain (build only) | 1.27 | `go.mod` `go 1.27`; build elsewhere is acceptable |
| Node.js Linux x64 | v22.23.4 (LTS Jod), npm 10.9.9 | nodejs.org `SHASUMS256.txt`; linux-x64 `.tar.xz` SHA-256 `1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce` |
| PI SDK | 1.5.0 | `bridge/package.json`; `internal/bridge/pi_sdk.go` |
| esbuild | 0.28.1 | `bridge/package.json` |
| pi-web-access | 0.47.0 | installed package metadata; engine Node >=22.19.0 |
| pi-mcp-adapter | 7.0.1 | installed package metadata; engine Node >=20 |
| ai-memory Linux x64 | v3.4.3 | GitHub release asset `ai-memory-linux-x86_64.tar.gz`, SHA-256 `2292e1e12895b48962513fee05ce535fadce5a63c9b83b2b3d9cb35fb0a4223e` |
| ai-jail Linux x64 | v3.2.0 | GitHub release asset `ai-jail-linux-x86_64.tar.gz`, SHA-256 `fd93fb499d087fc0c83412af9619c4e038a2e3aedbe1bc3caf0ff1cd306e778a` |
| bubblewrap | Debian 12 package, version recorded at install | Verify with `bwrap --version` before activation |

The installed Mac ai-memory is 3.4.2; the NAS personal instance will use the
pinned 3.4.3 release. This is not an automatic upgrade of the Mac instance.

Environment observed on 2026-11-07:

| Item | Value |
|---|---|
| NAS | UGREEN DXP2800, Intel N100, 4 cores, x86_64, Debian 12, kernel 6.18.15 |
| Memory / storage | 7.5 GiB RAM (about 3.4 GiB available at probe); ~1.4 TB free on ext4 |
| Existing runtime | System Node 18.20.4; `npm` absent; Docker CE and Compose present |
| Installed runtime | Node v22.23.4 + npm 10.9.9 under `/home/<nas-user>/.local/node/bin` |
| Service manager | systemd user running; `Linger=yes` for `<nas-user>` |
| Web probe | Node fetch to public HTTPS succeeded; Exa MCP search returned sources; DuckDuckGo returned a bot challenge |

## 3. Approved parameters

Confirmed by the user on 2026-11-07:

| Parameter | Decision | Status |
|---|---|---|
| Service account | `<nas-user>` (existing personal owner), with systemd/ai-jail hardening | Approved after UGREEN home-privacy blocker |
| Personal root | `/home/<nas-user>` | Existing |
| Workspace / default CWD | `/home/<nas-user>/Aurelia` | Created; mode 775 |
| File access | Read/write across the personal root, excluding `#recycle`, `lost+found`, hidden service/credential directories (`.ssh`, `.config`, `.local`, `.pi`, `.cache`, `.npm`, `.docker`, ai-memory data), and service-managed state | Approved with exclusions |
| Aurelia state | `/home/<nas-user>/.aurelia` (`AURELIA_HOME`) | Approved; same UID as the personal owner |
| PI agent dir | `/home/<nas-user>/.aurelia/pi-agent` | Approved |
| Node/npm | `/home/<nas-user>/.local/node` | Approved |
| ai-memory data | `/home/<nas-user>/.local/share/ai-memory` | Approved; hidden from agent file access by policy/jail |
| Local memory extraction | ai-memory consolidation (Dream/Nudge removed) | Approved; single backend |
| Model/provider | Same provider/model family as current, new key provisioned by private file | Approved |
| Staging surface | Headless tests without production Telegram token; no test bot | Approved |
| Backup destination | Continue under `/home/<nas-user>/.aurelia/backup-test` for now; no second disk is available | Approved interim; not a long-term off-volume backup |
| Cutover window | After phase 4 acceptance | Pending approval |

The personal-root grant is intentionally broad, per user choice. It does not
grant NAS administration, Docker control, SSH agent, or service configuration.
The ai-jail profile and Aurelia policy must still deny the exclusions and any
path outside the approved root. `#recycle` and hidden service/credential
directories are never read or written by the assistant.

## 3.1 Personal memory import (historical)

> **Historical cutover record:** the legacy user-global files below were later
> migrated into the personal ai-memory as `legacy-import` pages and the legacy
> runtime layers were removed. The current memory contract is section 15.6.

The user approved importing all personal memory. The source is the Mac
user-global memory layer:

`/Users/<mac-user>/.aurelia/users/<persona-user-id>/memory/`

Included: `MEMORY.md` and 13 markdown notes (93,323 bytes total, including the
index). Also included: `/Users/<mac-user>/.aurelia/users/<persona-user-id>/personas/USER.md`
(151 bytes).

Excluded: `.DS_Store`, `.dream-last`, `memory_receipts.jsonl` (operational
receipts), private/group topic directories (no private topic directory exists
for this account), project overlays under `~/.aurelia/projects/`, sessions,
credentials, caches, and coding ai-memory pages.

The inventory contains the user's personal notes; their contents are not
reproduced in this public document. They are user-global personal notes, not the
coding wiki, and are included by the user's choice. A secret-like pattern scan
of the 14 markdown files found no matches; this is a guardrail, not a guarantee
that every note is free of sensitive personal data. The originals remain
untouched.

## 4. Baseline

Recorded on 2026-11-07, metadata only (no chat content, prompts, or secrets):

| Surface | Baseline |
|---|---|
| Mac Aurelia | launchd label `com.aurelia.agent`, running before cutover |
| Mac cron store | 9 jobs, 280 execution rows; job prompts were not read |
| NAS existing services | existing personal services; not modified |
| NAS screen/session | `systemd --user` running; lingering disabled |
| Coding ai-memory | Mac instance 3.4.2; separate from this plan |

There is **no return path to the Mac** after cutover. Cutover is one-way:
stop the Mac consumer, prevent launchd from restarting it, preserve Mac data,
then activate the NAS. If the NAS fails after cutover, stop the NAS service,
preserve its data, and recover through backup/restore; do not reactivate the
Mac as assistant and do not sync two live databases. Backups are therefore the
only recovery mechanism.

## 5. Data import inventory

The approved personal import is limited to the user-global memory and persona
listed in section 3.1. The NAS personal ai-memory starts otherwise empty.

Do not import PI sessions, `sessions.json`, project bindings, continuity,
runlogs, cron jobs, caches, binaries, group-topic memory, project overlays, or
coding wiki pages. The import is a file copy into the personal runtime; it is
not a merge into the coding instance.

Before import: re-scan the approved files for secret-like patterns, preserve the
originals, and record hashes. After import: compare hashes and confirm the
files are visible only in the personal scope.

## 6. Phase checklist

The authoritative checklist is
`openspec/changes/nas-personal-assistant/tasks.md`.

1. Phase 0 — parameters, version matrix, import inventory, baseline.
2. Phase 1 — service account, Node/npm, Linux artifact, systemd, lingering.
3. Phase 2 — personal ai-memory, PI/extensions, scope canary, capture policy,
   backup restore.
4. Phase 3 — ai-jail/bubblewrap, headless profile, workspace policy, protected
   path test, systemd integration.
5. Phase 4 — real web search/fetch/document, failure cases, restart/logout,
   resource measurement and backup/restore rehearsal (no Mac return).
6. Phase 5 — selective import, cutover approval, single consumer, job
   recreation, acceptance and ongoing operations.

## 7. Verification log

| Date | Check | Result |
|---|---|---|
| 2026-11-07 | NAS network and Exa search from Node | Pass (HTTP 200, sources returned) |
| 2026-11-07 | NAS public HTTPS fetch | Pass |
| 2026-11-07 | DuckDuckGo from NAS | Bot challenge (not a sole provider) |
| 2026-11-07 | User namespaces / Landlock / seccomp | Available; ai-jail not yet installed |
| 2026-11-07 | Personal root and alias | Same inode; RW for `<nas-user>` |
| 2026-11-07 | Mac launchd and cron metadata | Recorded; no production change |
| 2026-11-07 | Version matrix and checksums | Recorded in section 2 |
| 2026-11-07 | Personal memory inventory | 14 markdown files + USER.md; no secret-like pattern matches; originals untouched |
| 2026-11-07 | User decisions | File access, import, model key, staging, backup deferral recorded in section 3 |
| 2026-11-07 | UGREEN home-privacy blocker | Confirmed `ugacl_vfs`/`ug_posix_acl`; temporary `aurelia` account removed; decision: run as `<nas-user>` with hardening |
| 2026-11-07 | Node/npm install | v22.23.4 / npm 10.9.9 at `/home/<nas-user>/.local/node/bin`; checksum verified |
| 2026-11-07 | Linux Aurelia binary | v0.51.0 ELF amd64 at `/home/<nas-user>/.aurelia/bin/aurelia`; SHA-256 recorded |
| 2026-11-07 | systemd unit | Rendered, inactive/disabled, `systemd-analyze --user verify` passed |
| 2026-11-07 | Lingering | `Linger=yes`; user manager running |
| 2026-11-07 | ai-memory personal service | 3.4.3 active on loopback; auth token 600; data dir 700; project `personal/aurelia` |
| 2026-11-07 | PI extensions | ai-memory extension, pi-web-access 0.47.0, pi-mcp-adapter 7.0.1 installed |
| 2026-11-07 | Synthetic LLM call | `deepseek/deepseek-v4-pro` returned `OK`; existing NAS credential |
| 2026-11-07 | Personal memory canary | `sessions/nas-canary-1794068160.md` written/read/searched under `personal/aurelia` |
| 2026-11-07 | PI memory tool | `memory_query` succeeded once; later runs showed zero registered memory tools — recorded as known issue |
| 2026-11-07 | Capture exclusion | Synthetic excluded path produced 0 observations; control path produced 1; exclusion works |
| 2026-11-07 | Backup/restore | 112.6 KiB official backup restored into isolated `restore-test`; canary page recovered; live service restarted |
| 2026-11-07 | ai-jail/bubblewrap | bubblewrap 0.8.0 + ai-jail 3.2.0; network probe HTTP 200; Landlock enforced |
| 2026-11-07 | ai-jail profile | `--rw-map $HOME`, `--network`, deny `.ssh`/`.config`/`.docker`/ai-memory/`#recycle`; `net-ok`, `write-ok`, `ssh-denied`, `memory-denied` |
| 2026-11-07 | Protected-path denial | Synthetic sentinel write denied; SHA-256 unchanged |
| 2026-11-07 | systemd unit | Re-rendered with ai-jail wrapper, `WorkingDirectory=/home/<nas-user>/.aurelia`; later activated in staging |
| 2026-11-07 | Root-owned launcher | `/usr/local/bin/ai-jail` and `/usr/local/lib/aurelia/aurelia-jail` root:root; unit uses the wrapper |
| 2026-11-07 | Staging daemon | Active with Telegram/cron disabled; IPC listening; stop 0s and no orphan processes |
| 2026-11-07 | Tool surface | `pi_active` includes web and memory tools; web search + fetch executed through the daemon |
| 2026-11-07 | Integrated document | `research-smoke.md` (913 bytes, 1 Debian URL) written in workspace and second approved folder |
| 2026-11-07 | Memory failures | ai-memory stopped → `MEMORY_UNAVAILABLE`; invalid fetch → `FETCH_FAILED`; no coding fallback |
| 2026-11-07 | Restart persistence | Canary query and file survived restart; ai-memory ~129 MB, Aurelia ~13 MB, ~3.3 GiB free |
| 2026-11-07 | Personal import | 15 pages imported to `personal/aurelia/imported/`; originals preserved; staging removed |
| 2026-11-07 | Security review | Fixed offline Stop deadlock, symlink-safe instance lock, fail-closed installer, root-owned launcher, env forwarding, fail-closed flags; full Go/bridge tests pass |
| 2026-11-07 | Graceful stop | `ExitType=cgroup`; daemon logged "Shutting down Aurelia", no SIGKILL or leftover process |
| 2026-11-07 | Telegram token | Copied from Mac to NAS over an SSH stdin pipe; SHA-256 matched; token not printed; staging remains disabled |
| 2026-11-07 19:05 | Cutover | Mac launchd stopped/disabled, no Aurelia/bridge process, deploy hook moved; NAS Telegram started as sole consumer; test message id 2514 sent |
| 2026-11-07 19:05 | Live validation | User Telegram message handled by NAS: run 17 completed, 0 failed; no 409/conflict |
| 2026-11-07 19:09 | Telegram group | `getChat` confirmed supergroup `<telegram-group-name>` (<telegram-group-id>); bot is a member; invalid positive ID removed from allowed groups; no new group needed |
| 2026-11-07 19:09 | Group context | Project binding/cwd, session history and cron are not migrated; group starts fresh; set `/cwd /home/<nas-user>/Aurelia` in the group for file/document work |
| 2026-11-07 20:21 | Desktop tunnel | `com.aurelia.nas-tunnel` installed and running; `~/.aurelia/nas.sock` mode 600; SSH streamlocal over Tailscale; reconnect after killing `ssh` restored the socket in ~1s |
| 2026-11-07 20:21 | Desktop IPC identity | `sessions`, `history` and `project_state` succeeded with `user_id=0`; `user_id=501` rejected with peer UID 1000; no daemon auth relaxation |
| 2026-11-07 20:24 | Desktop online | App opened against the NAS; `bridge online`, project `Aurelia`, `/home/<nas-user>/Aurelia` DM binding; no local `aurelia.sock` |
| 2026-11-07 | `/topic init` | Slug normalization/path traversal/marker/idempotency tests pass; existing topic `/topic init` is a no-op; bot does not need Manage Topics |
| 2026-11-07 | Desktop build/tests | Tauri debug build succeeded; `aurelia-ipc` 5/5 tests, desktop lib test 1/1; installed at `~/.aurelia/bin/aurelia-desktop` |
| 2026-11-07 | TUI disabled | No `aurelia-tui` process on Mac or NAS; desktop continues to use the TUI session namespace; `tui_sessions.db` retained |

## 8. Resolved blocker: personal-root access

The NAS enforces home-directory privacy with the UGREEN kernel modules
`ugacl_vfs` and `ug_posix_acl`. Evidence from 2026-11-07:

- `/home/<nas-user>` is mode `0777` and owner `<nas-user>:admin` when inspected by
  `<nas-user>`.
- The same directory appeared as mode `0770` to a temporary `aurelia` account
  (uid 1001, gid 1001), and that account could not traverse it.
- Adding a POSIX ACL for `aurelia` on `/home/<nas-user>` and
  `/home/<nas-user>/Aurelia` did not change that view; the ACL was removed.
- Group `admin` (gid 10) is granted `ALL=(ALL:ALL)` in `/etc/sudoers`.
  Adding `aurelia` to `admin` would grant full sudo and was rejected.
- The temporary `aurelia` account was removed. `/home/<nas-user>/Aurelia`
  remains, mode 775, owned by `<nas-user>:admin`.

Decision: run the personal assistant as `<nas-user>`, with systemd/ai-jail
hardening (`NoNewPrivileges`, empty capability bounding set, hidden Docker
socket, `.ssh`, service/credential directories, no SUID/SGID). This provides
the requested full personal-root access but is **not** an OS privilege boundary
against hostile code. ai-jail remains accident prevention. This residual risk
is accepted for the personal assistant; secrets and service state must still be
excluded from agent file access wherever the runtime allows it.

## 9. Current installed state (2026-11-07)

| Component | State |
|---|---|
| Node/npm | v22.23.4 / npm 10.9.9 under `/home/<nas-user>/.local/node/bin` |
| Aurelia binary | v0.53.0 Linux amd64 under `/home/<nas-user>/.aurelia/bin/aurelia`; SHA-256 `291ec8cd4dd7e1cb2c65b8de1b4dd44093b7b1dbefe0982a51406d880388d970`; previous binary backed up as `aurelia.prev-20261107213352` |
| Aurelia service | **active**; Telegram and cron enabled; `ExitType=cgroup`; IPC socket listening; cron store has 0 jobs; stop logs graceful shutdown with no orphans |
| ai-jail | 3.2.0; bubblewrap 0.8.0; `/usr/local/bin/ai-jail` and `/usr/local/lib/aurelia/aurelia-jail` are root-owned; unit `ExecStart` points to the root-owned wrapper |
| Lingering | `Linger=yes` for `<nas-user>`; user manager running |
| ai-memory | 3.4.3 active on `127.0.0.1:49374`; data under `/home/<nas-user>/.local/share/ai-memory`; token/config 600, data dir 700; db/wiki/raw/config denied to the jail |
| ai-memory projects | `personal/aurelia` with 24 pages at last check; 15 personal pages imported under `imported/`; per-topic projects are created on demand |
| PI runtime | PI SDK 1.5.0, pi-web-access 0.47.0, pi-mcp-adapter 7.0.1; the daemon agent dir keeps a real npm tree with transitive dependencies |
| Tool surface | `web_search`, `fetch_content`, `get_search_content`, `memory_query`, `memory_write_page` and the rest of the allowlist registered and exercised through the daemon |
| Model test | `deepseek/deepseek-v4-pro` returned `OK`; web research and memory tools exercised through the IPC surface |
| Telegram | Cutover executed on 2026-11-07 19:05 WET; Mac daemon stopped/disabled; NAS is the sole consumer; live reply validated (run 17, no 409) |
| Telegram topics | Bindings for General plus four named topics; each topic has its own cwd/marker; `/topic init <slug>` creates new isolated projects |
| Desktop (Mac) | Tauri debug binary `~/.aurelia/bin/aurelia-desktop`; NAS mode via `~/.aurelia/nas.sock`; app shown online with project `Aurelia` |
| Mac tunnel | launchd `com.aurelia.nas-tunnel` running; `nas.sock` mode 600; SSH streamlocal over Tailscale; reconnect after kill validated in ~1s |
| SSH forwarding | `AllowTcpForwarding local` and `AllowStreamLocalForwarding yes`; original config backed up at `/etc/ssh/sshd_config.aurelia-backup-20261107` |
| TUI | Client disabled; the desktop still uses the TUI session namespace (`~/.aurelia/data/tui_sessions.db`, chat IDs `-9000001…`), so that database must not be deleted |

The provider/model test used the existing NAS `deepseek` credential. The
`opencode-go` key remains an open decision; the current runtime uses the
configured NAS model/profile. The desktop model badge is a client selection;
use `/model` in the desktop to align with the Telegram profile.

## 10. Resolved tool-registration issue and residual risks

The missing `web_search`/`fetch_content` had two causes: the daemon agent dir
only had package symlinks, so `pi-web-access` could not import `p-limit` and
other dependencies, and async extension registration was not given a bounded
window. The fix installs the real package tree under
`~/.aurelia/pi-agent/npm`, makes `EnsureBridge` preserve real package
installations, and waits up to 5 seconds for `web_search`, `fetch_content`,
`memory_query` and `memory_write_page` before the first turn. The ai-memory
extension also needed the token file; the jail now exposes `auth-token` while
denying `db`, `wiki`, `raw`, `config.toml`, `models`, `logs`, and `auth-tokens`.

Verified through the daemon: web search + source fetch, a Markdown document in
two approved folders, a memory canary written/queried after a restart, memory
unavailable with no fallback to the coding server, and invalid fetch reporting
`FETCH_FAILED`.

Residual risks accepted for this personal deployment:

- The systemd **user unit** and the `staging.env` file live under `<nas-user>`'s
  home and are writable by code running as that user. The root-owned wrapper
  prevents environment redirection of `ai-jail`, but a compromised agent could
  still edit its own service definition outside the jail. This is not a
  hostile-code boundary; use a root-owned system unit if that threat matters.
- Files the daemon must read remain readable to same-UID code: `~/.pi/agent/auth.json`,
  `models.json`, `~/.aurelia/config/app.json`, and `~/.aurelia/config/staging.env`.
  The jail hides `.ssh`, `.config`, Docker, the ai-memory db/wiki/raw/config and
  session caches, but it does not make credentials unreadable to arbitrary
  build/test code.
- The personal root is intentionally read/write. Media backup and development
  folders are not capture targets, but they are reachable by the agent. The
  internal policy and ai-jail reduce accidents, not malicious use.
- The interim backup lives on the same volume (`~/.aurelia/backup-test`); it is
  not an off-volume disaster-recovery copy.

## 11. Open decisions

- Provision the approved `opencode-go` credential (new key) and choose the
  exact model; the current runtime uses the existing NAS credential.
- Implement byte uploads for desktop images/documents in NAS mode; today the
  client sends Mac-local paths that the NAS cannot read.
- Replace the interim same-volume backup when another disk or network backup
  destination is available.
- Whether the broad personal-root grant needs a narrower review after the first
  real tasks reveal which folders are actually used.
- Next release: accumulate the next set of changes on a new feature branch from
  `main` v0.53.0; do not reuse the merged feature branch.

**No return to Mac:** cutover is one-way. If the NAS fails, the service is
stopped, data is preserved, and recovery is through backup/restore only.

## 12. Remaining implementation state

31/31 original tasks + 4/4 desktop addendum complete. Phases 0–3, integrated web/file/memory rehearsal, product
validation, security review, job inventory, 6.3 (stop old consumer), 6.4 (live
Telegram validation) and 6.6 (acceptance) are complete. The Mac daemon is
stopped and disabled; the NAS is the sole Telegram consumer as of 2026-11-07
19:05 WET. The desktop connects to the same NAS daemon through the Tailscale
tunnel; the TUI client is disabled. No return to Mac.

The v0.53.0 release is complete: merged as `d44b319`, tagged `v0.53.0`, NAS
redeployed and the desktop reconnected. The next release is TBD; start a new
feature branch from `main`.

## 13. Cutover window C

Approved window: **Sunday 2026-11-08, 09:05–09:55 WET/GMT** (Europe/Lisbon).
The user requested an early cutover; it was executed on **2026-11-07 19:05 WET**.
No return to Mac.

Preconditions already prepared:

- Telegram token copied over SSH and hash-verified; not printed.
- Real identity prepared: user `<persona-user-id>`, profile with
  `default_cwd=/home/<nas-user>/Aurelia`, personas copied, staging still disabled.
- Pre-cutover backup 404,069 bytes copied to the Mac; SHA-256
  `51b49f845f14a4caf62fe5db3fa7142bb33264051f8e9e9ab85bfd9f350f7771`.
- All 9 Mac cron jobs are inactive (`active=0`); NAS cron store has 0 jobs.
- Before the window, the Mac was the only Telegram consumer. The checklist
  below is retained as the executed record.

Window checklist:

1. Create a fresh `ai-memory backup` and copy it to the Mac; verify hashes.
2. Stop the Mac consumer: `launchctl bootout gui/$(id -u)/com.aurelia.agent`
   then `launchctl disable gui/$(id -u)/com.aurelia.agent`; verify no Aurelia
   process remains. Check and neutralize any post-commit deploy hook that could
   restart it.
3. On the NAS, rename `~/.aurelia/config/staging.env` to
   `staging.env.cutover-disabled-20261107-1905`; restart `aurelia.service`.
4. Confirm Telegram polling starts without 409 conflict, cron starts with an
   empty store, and the real user/profile is used.
5. Validate via Telegram: message reply, web search/source, document write,
   memory query. Inspect logs for permission/credential errors.
6. Observe the agreed period. There is no rollback: on failure stop the NAS,
   preserve its data, restore the backup, and leave the Mac disabled.

## 14. Adding new Telegram groups

A Telegram bot token cannot create groups. Create the group with your Telegram
account, then add `@<bot-handle>` to it. Aurelia supports multiple groups, but
there is **no automatic authorization**:

1. Obtain the numeric group chat ID. Public groups can be looked up by
   `@username` with `getChat`; for private groups use a helper such as
   `@RawDataBot`, or pause the NAS bot briefly and read `getUpdates`.
2. Add the ID to `telegram_allowed_group_ids` in
   `/home/<nas-user>/.aurelia/config/app.json` and restart `aurelia.service`.
3. The sender must also be in `telegram_allowed_user_ids`; a whitelisted group
   does not authorize every member.
4. In the group, use `/cwd --group /home/<nas-user>/Aurelia` to bind the whole
   group, or `/cwd /home/<nas-user>/Aurelia` for the current topic. Without a
   cwd the group runs in chat mode without file tools.
5. If the bot should see ordinary group messages rather than only commands and
   mentions, disable privacy mode in BotFather or make it an admin.

Existing Telegram groups are not recreated by a cutover; only their
authorization and the assistant's local context are separate. A new group
starts with a fresh session and no inherited Mac context.

Inside an already allowed **forum supergroup**, new topics need no allowlist
change or restart. `<telegram-group-name>` is a forum (`is_forum=true`): each topic is a
separate thread with its own PI session and ai-memory project. A group-level
`/cwd --group /home/<nas-user>/Aurelia` provides the default; `/cwd` inside a
topic overrides it. Topics that share the same cwd also share the ai-memory
`personal/aurelia` project.

Initial NAS state (2026-11-07, historical): no `~/.aurelia/topics/` directory,
zero `conversation_project_binding` rows, and one group-level continuity row
from the first interaction. Telegram topics may exist in the group, but Aurelia
topic-specific state is created on first use. The legacy continuity/topic
layers have since been migrated to ai-memory and removed.

The **private DM does not need `/cwd`**: the user profile has
`default_cwd=/home/<nas-user>/Aurelia`, applied automatically because
`DefaultCWD` is a private-chat/thread-0 fallback. A manual `/cwd` overrides it;
`/cwd clear` removes the override and restores the profile default. Groups do
not use `DefaultCWD`; they need `/cwd --group` or a per-topic `/cwd`.

**Memory scope:** ai-memory is scoped by workspace/project derived from the cwd
and `.ai-memory.toml` marker. Topics sharing the same cwd share the same
ai-memory project (`personal/aurelia` for the main workspace); sessions and
captured observations are per thread but durable pages are project-scoped. For
isolated topics, use distinct cwds with distinct `.ai-memory.toml` projects;
otherwise accept cross-topic recall.

Current Telegram topic map for `<telegram-group-name>` (`<telegram-group-id>`).
Private thread IDs, topic labels and slugs are intentionally omitted from this
public document; the authoritative mapping is in
`~/.aurelia/config/app.json` and `~/.aurelia/data/project_bindings.db`.

| Topic | Thread ID | cwd | ai-memory project |
|---|---:|---|---|
| General | 0 | `/home/<nas-user>/Aurelia/groups/<group-slug>/general` | `personal/aurelia-group` |
| Topic A | `<thread-id-a>` | `/home/<nas-user>/Aurelia/topics/<topic-a>` | `personal/aurelia-<topic-a>` |
| Topic B | `<thread-id-b>` | `/home/<nas-user>/Aurelia/topics/<topic-b>` | `personal/aurelia-<topic-b>` |
| Topic C | `<thread-id-c>` | `/home/<nas-user>/Aurelia/topics/<topic-c>` | `personal/aurelia-<topic-c>` |
| Topic D | `<thread-id-d>` | `/home/<nas-user>/Aurelia/topics/<topic-d>` | `personal/aurelia-<topic-d>` |

The DM stays on `/home/<nas-user>/Aurelia` / `personal/aurelia`. These are
logical project boundaries, not a hard security boundary: the same ai-memory
server and token can still query other projects when explicitly asked.

For a new topic, use `/topic init <slug>` inside it. The command:
- accepts accents/spaces and normalizes to a safe slug;
- creates `<default_cwd>/topics/<slug>`;
- writes the `.ai-memory.toml` marker with `workspace` from the group marker
  (default `personal`) and project `aurelia-<slug>`;
- writes a topic-specific binding and invalidates the overlay cache;
- is idempotent for the same slug/project and refuses an existing unmarked
  directory or a conflicting marker.

It requires a group/topic (not DM or General) and the profile `default_cwd`.
The bot does not need Manage Topics for `/topic init`; that permission is only
needed if it should create the Telegram topic itself.

## 15. Mac desktop over Tailscale (2026-11-07)

The Mac desktop client talks to the NAS daemon through an SSH **streamlocal**
tunnel, not a TCP port. The tunnel exposes the NAS Unix socket at
`~/.aurelia/nas.sock` on the Mac; the desktop selects it when
`~/.aurelia/nas-tunnel.enabled` exists. Without the marker it uses
`~/.aurelia/aurelia.sock` (local daemon). This keeps the local socket free and
avoids clobbering a local daemon.

### 15.1 NAS sshd

The UGREEN `/etc/ssh/sshd_config` shipped with `AllowTcpForwarding no`.
OpenSSH 9.2 denies `direct-streamlocal` in that configuration, even with
`AllowStreamLocalForwarding yes` (verified with a temporary socket: the server
logged `request was denied`). The NAS now has:

```text
AllowTcpForwarding local
AllowStreamLocalForwarding yes
```

`local` allows only client-to-server forwarding (`ssh -L`), not remote
forwarding (`ssh -R`). The user's SSH key already grants a shell, which is
strictly more powerful than forwarding, so this is not a privilege escalation.
The pre-change file is at
`/etc/ssh/sshd_config.aurelia-backup-20261107`.

Apply/reload/verify:

```bash
sudo sshd -t
sudo systemctl reload ssh
sudo sshd -T -C user=<nas-user>,host=<nas-host> | \
  grep -E 'allowtcpforwarding|allowstreamlocalforwarding'
```

Revert with:

```bash
sudo cp -a /etc/ssh/sshd_config.aurelia-backup-20261107 /etc/ssh/sshd_config
sudo sshd -t
sudo systemctl reload ssh
```

### 15.2 Mac install

On the Mac, with the SSH key already in the login keychain:

```bash
make install-macos-nas-tunnel
```

This installs:

- `~/Library/LaunchAgents/com.aurelia.nas-tunnel.plist` — launchd agent;
- `~/.aurelia/bin/aurelia-nas-tunnel` — wrapper;
- `~/.aurelia/nas-tunnel.env` — host/socket settings;
- `~/.aurelia/nas-tunnel.enabled` — desktop marker;
- `~/.aurelia/nas.sock` — local end of the tunnel.

The wrapper uses `ssh -N -L <local.sock>:<remote.sock>`, `UseKeychain=yes`,
`BatchMode=yes`, `ExitOnForwardFailure=yes`, keepalives and
`StrictHostKeyChecking=yes`. It does not create or copy keys. The repository
defaults are generic (`AURELIA_NAS_HOST=aurelia-nas`,
`AURELIA_NAS_USER=aurelia`, remote socket
`/home/$AURELIA_NAS_USER/.aurelia/aurelia.sock`). For the real NAS:

```bash
AURELIA_NAS_HOST=<nas-host> AURELIA_NAS_USER=<nas-user> \
  make install-macos-nas-tunnel
```

The installer writes the selected values to `~/.aurelia/nas-tunnel.env`; the
wrapper reads that file at every launch. The real values never need to be
committed to the repository.

If a local `aurelia` daemon is running, the wrapper refuses to start and logs
the reason; the NAS tunnel must not be confused with local state. Remove the
tunnel before re-enabling the local daemon:

```bash
make uninstall-macos-nas-tunnel
```

### 15.3 Identity and project

The desktop now sends `user_id=0`; the daemon's TUI handler maps it to
`default_persona_user_id` (the same persona used by Telegram). The previous
client sent the Mac UID (501), which the NAS correctly rejected because the
SSH-forwarded peer UID is 1000 (`<nas-user>`). No daemon authentication check was
weakened.

The TUI DM binding (`chat_id=-9000001`, thread 0) was set to
`/home/<nas-user>/Aurelia`, so the desktop shares the personal hub and the
`personal/aurelia` ai-memory project with the Telegram DM. Verify with `/cwd`
or the project panel. The desktop uses the TUI session namespace
(`~/.aurelia/data/tui_sessions.db`, chat IDs `-9000001…`) even though the TUI
client itself is disabled; do not delete that database. The NAS database is
currently empty, so the implicit DM is used.

### 15.4 Verification

```bash
launchctl print gui/$(id -u)/com.aurelia.nas-tunnel | sed -n '1,40p'
ls -l ~/.aurelia/nas.sock          # srw-------
tail -f ~/.aurelia/logs/nas-tunnel.log
```

In the app: `daemon_status` should show online; `/status` should show the
model/cwd; `/cwd` should show `/home/<nas-user>/Aurelia`. When the Mac wakes or
changes network, launchd restarts ssh; the next app request reconnects.

### 15.5 Limitations

- **Attachments:** the desktop protocol sends local file paths, and the NAS
  daemon reads those paths on the NAS. Images/documents selected on the Mac do
  not work in NAS mode; text, slash commands and streaming do. Use Telegram
  for attachments until the protocol uploads bytes (base64 or an upload
  endpoint). Images already support the base64 `data` field in the IPC type,
  but the desktop currently sends only `path`.
- **Tailscale:** the tunnel needs Tailscale connected and the SSH host alias
  (`<nas-host>` by default) resolving. It works at home and outside.
- **Host key:** `StrictHostKeyChecking=yes` means a NAS reinstall with a new
  host key requires updating `~/.ssh/known_hosts`.
- **Keychain:** `UseKeychain=yes` needs the key in the login keychain; if the
  agent/keychain is locked, the launch agent logs a failure and retries.

### 15.6 Memory sharing between desktop and Telegram

A new desktop session pointed at a Telegram topic cwd is a **new conversation
inside the same project**, not a continuation of the Telegram thread. New
desktop sessions do not inherit the DM or the topic binding; run `/cwd` in
each session.

| Layer | Shared? | Detail |
|---|---|---|
| cwd and files | **Yes** | Both surfaces read/write the same project directory. |
| ai-memory project | **Yes** | The topic `.ai-memory.toml` marker selects the project (for example `personal/aurelia-<topic>`). Pages/observations/handoffs written there are available to both sides. This is the only durable memory path. |
| PI session/transcript | **No** | The desktop session has its own `chat_id` and session file; it does not load the Telegram topic history. |
| `/cwd` binding | **No** | Each chat/session has its own binding. Setting the desktop cwd does not change the Telegram topic, and vice versa. |
| Session list | **No** | The desktop lists TUI-namespace sessions; the Telegram topic is not a desktop session. |
| Model | **No** | Model is per session/config; use `/model` in the desktop to align with the Telegram profile. |

Practical consequences:

- To continue the **project**, run `/cwd` in the desktop session and rely on
  the shared ai-memory project. To continue the **exact Telegram
  conversation**, use the Telegram topic; its PI session file is not
  transferred.
- `/memory checkpoint` no longer exists: the durable note path is a page
  write/handoff in ai-memory, and the desktop reads the bounded project working
  set on its next turn. `/memory` reports project pages, observations,
  sessions, pending handoffs and degraded state.
- `/reset` clears the PI session and expires pending ai-memory handoffs for the
  conversation, so the next session does not consume a handoff created before
  the reset.
- Changing `/cwd` to a different ai-memory project resets the PI session for
  that conversation, so the ai-memory session row, observations and handoff
  belong to the new project. A session created before the topic binding was set
  keeps the old project attribution until `/reset` (or a later `/cwd` change)
  starts a new session.
- The desktop and the Telegram topic can run concurrently. There is no
  cross-surface lock over the shared files; avoid simultaneous edits to the
  same file.
- Current project-topic state (2026-11-07): one existing topic has a PI session
  file and an ai-memory project. The private mapping is in
  `~/.aurelia/config/app.json` and `project_bindings.db`.

### 15.7 ai-memory Pi extension handoff

The shared Pi extension `ai-memory-pi.ts` injects the pending handoff at
`before_agent_start` and posts session-start, prompt, tool, stop, pre-compact
and session-end events with the PI session id. Handoff creation/consumption is
therefore automatic and project-scoped; do not reimplement it in the daemon.

The Pi extension does **not** inject a project briefing/pinned digest. The thin
`internal/memorycontext` adapter reads `memory_briefing`/`memory_read_page`
through the loopback MCP endpoint, formats a bounded working set (counts, rules,
recent page excerpts and pending handoff count), and never duplicates the
handoff. ai-memory is now the unconditional backend; there is no legacy,
shadow or env-selected provider path. The personal instance is the only
backend: Aurelia never falls back to the coding ai-memory instance, and the
ai-memory capture exclusions remain enforced in the shared configuration.

### 15.8 Memory consolidation rollout (2026-11-07)

The legacy memory migration is complete: 23/23 pages were written and verified
in the personal ai-memory, tagged `legacy-import`. The pre-import backup is
`~/.aurelia/backup-test/pre-memory-consolidation-20261107224526.tar.gz`
(SHA-256 `c51be982b22bced158d8f5ae2217c8759d8e47d54c3466d047a2fa4a8d91d07a`).
The original files/continuity DB were only read.

The live NAS daemon switched to the ai-memory working set on 2026-11-07. The
first live turn logged
`memory: ai-memory working set project=personal/aurelia chars=3126 sources=3
degraded=false pending_handoffs=1`, a TUI smoke turn replied `OK`, and the
desktop showed `bridge online` with the `personal/aurelia` scope.

The legacy runtime stack (continuity, `cwd_overlay`/`topic`/`user_global`,
memoryux, Dream/Nudge and the old migration commands) was then removed from
the daemon. The original files may remain on disk for audit/rollback, but the
runtime no longer reads or writes them.

Rollback after the removal uses the binary/config backups, not a provider flag:
`~/.aurelia/config/app.json.pre-memory-switch-20261107230812`,
`~/.aurelia/bin/aurelia.prev-memory-switch-20261107230812`,
`~/.aurelia/bin/aurelia.pre-cwd-reset-20261107232917`, and the pre-removal
binary `~/.aurelia/bin/aurelia.pre-legacy-removal-20261108000833` (SHA-256
`c1a886c4ced741afe3a7aa52522925e7793ae8ce7d5ee57c9c2abbabb4976cd0`). Restore
the previous binary and restart `aurelia.service` to return to the legacy
stack.

The `/memory` command reports ai-memory project/page/handoff state. The local
checkpoint command is gone, and `/reset` clears the PI session and cancels
pending handoffs instead of resetting continuity.

## 16. Daily operations

### 16.1 NAS services

```bash
systemctl --user status aurelia.service ai-memory.service
systemctl --user restart aurelia.service
journalctl --user -u aurelia.service -n 100 --no-pager
journalctl --user -u ai-memory.service -n 100 --no-pager
```

The daemon and ai-memory run as the personal `<nas-user>` account with
lingering enabled. The ai-memory listener is loopback-only; the token and data
directories stay owner-only. Telegram is the only consumer of the production
bot token.

### 16.2 Desktop (Mac)

```bash
launchctl print gui/$(id -u)/com.aurelia.nas-tunnel
tail -f ~/.aurelia/logs/nas-tunnel.log
```

`~/.aurelia/nas.sock` exists only while the tunnel is up. The desktop selects
it when `~/.aurelia/nas-tunnel.enabled` exists. If the app shows offline after a
wake/network change, wait for launchd to reconnect and use the app's refresh
action; the socket is recreated automatically. Remove the tunnel before
re-enabling a local daemon.

### 16.3 Telegram and desktop sessions

- New Telegram topic: `/topic init <slug>` inside the topic. It creates the
  directory, marker and binding; the existing topics need no action.
- New desktop session: create it, then `/cwd <path>`; new sessions do not
  inherit the DM/topic cwd. Use `/status` and `/model` from the composer.
- Cross-surface context: durable notes go to ai-memory pages/handoffs; the
  desktop reads the bounded project working set on its next turn. The raw
  transcript and PI session remain separate (section 15.6).

### 16.4 Backup

```bash
ai-memory backup --to <destination>
```

The interim backup lives on the same volume; copy the archive to the Mac or
another off-volume destination. The pre-cutover archive
(404,069 bytes, SHA-256
`51b49f845f14a4caf62fe5db3fa7142bb33264051f8e9e9ab85bfd9f350f7771`) is on the
Mac. No source data is deleted during backup/restore tests.

### 16.5 Release/update

**v0.53.0 is live** (`d44b319`, tag `v0.53.0`). For the next release, use a new
feature branch from `main`; after approval and merge:

1. Build the Linux binary from the merged commit and replace
   `~/.aurelia/bin/aurelia` atomically (`.new` + `mv`).
2. Restart `aurelia.service`; verify Telegram and the desktop can connect.
3. Re-run the desktop tunnel/IPC check and a Telegram reply/search check.
4. Keep the previous binary and the pre-cutover memory backup until the new
   release is accepted.

## 17. Known limitations and residual risks

- **Desktop attachments in NAS mode:** images/documents are sent as Mac-local
  paths and the NAS daemon cannot read them. Text, commands and streaming work;
  use Telegram for attachments until byte uploads are implemented.
- **Telegram topics:** each topic has its own PI session and ai-memory project
  selected by its cwd marker. Desktop sessions with the same cwd share the
  ai-memory project; they do not load the Telegram transcript.
- **Concurrency:** desktop and Telegram can run at the same time. There is no
  cross-surface lock; avoid simultaneous edits to the same files/overlay.
- **Model selection:** the desktop model badge is a client selection; the
  daemon profile/model can differ. Use `/model` to align.
- **Backup:** the interim copy is on the same volume. It is not an off-volume
  disaster-recovery copy; keep the Mac copy current.
- **Security boundary:** the daemon runs as the personal owner with ai-jail for
  accident prevention, not as a hostile-code boundary. The user-unit files are
  writable by same-UID code; see section 10.
- **Public documentation:** this public runbook uses placeholders for Telegram
  IDs, hostnames, Tailscale addresses and personal paths. Real values live in
  `~/.aurelia/config/app.json` and `~/.aurelia/nas-tunnel.env`.
- **TUI:** the client is disabled in this deployment, but the desktop uses the
  TUI session namespace. Do not remove `tui_sessions.db` or the session files
  referenced by `-9000001…`.
- **Per-topic ai-memory projects:** projects are created on demand. A topic
  with no pages yet is not a failure; the first write/observation registers the
  project.

## 18. Release and PR state

- Released: **v0.53.0** on 2026-11-07; merge commit `d44b319`; annotated tag
  `v0.53.0`.
- `main` is at `d44b319`; the NAS runs v0.53.0; the previous binary is backed
  up on the NAS.
- The v0.53.0 PR (#88) is merged; the feature branch is not reused for the next
  release.
- Next release: TBD; start a new feature branch from `main` v0.53.0.
- The TUI client remains disabled; desktop + Telegram are the supported
  personal surfaces.

