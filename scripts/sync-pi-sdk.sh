#!/usr/bin/env bash
# sync-pi-sdk.sh — keep the Aurelia bridge on the PI SDK version of the PI CLI.
#
# The bridge pins @earendil-works/pi-ai and @earendil-works/pi-coding-agent in
# two places that must agree: bridge/package.json (what npm typechecks and
# builds against) and internal/bridge/pi_sdk.go (what the daemon installs at
# runtime). This script moves both to the version the local PI CLI runs, then
# reinstalls, typechecks, tests, rebuilds the bundle and runs the Go bridge
# tests — so a bump arrives already verified.
#
# It NEVER commits, bumps the Aurelia version, or touches CHANGELOG.md: those
# need review and explicit approval (AGENTS.md).
#
# Usage:
#   scripts/sync-pi-sdk.sh                 # follow the installed PI CLI
#   scripts/sync-pi-sdk.sh --version 0.86.0  # pin an explicit version
#   scripts/sync-pi-sdk.sh --check         # report drift only (exit 1 if stale)
#   scripts/sync-pi-sdk.sh --force         # re-run validation with no version change
#
# Exit codes: 0 in sync / synced, 1 drift (--check), 2 usage or tooling error.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_DIR="${REPO_ROOT}/bridge"
REPO_MANIFEST="${BRIDGE_DIR}/package.json"
GO_PIN_FILE="${REPO_ROOT}/internal/bridge/pi_sdk.go"
DAEMON_SDK_MANIFEST="${HOME}/.aurelia/bridge/node_modules/@earendil-works/pi-coding-agent/package.json"

SDK_MANIFEST_REL="@earendil-works/pi-coding-agent/package.json"
PI_PACKAGES=("@earendil-works/pi-ai" "@earendil-works/pi-coding-agent")

info() { printf '==> %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 2; }

usage() {
	awk 'NR > 1 && /^#!/ { next } NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "${BASH_SOURCE[0]}"
}

# detect_cli_version prints the installed PI CLI version, or nothing when it
# cannot be determined. `pi --version` is authoritative when it prints a plain
# semver; otherwise fall back to the globally installed package manifest, since
# a wrapper or update notice can pollute the CLI output.
detect_cli_version() {
	local from_cli=""
	if command -v pi >/dev/null 2>&1; then
		from_cli="$(pi --version 2>/dev/null | tr -d '[:space:]' | sed -E 's/^v//' || true)"
		if [[ "${from_cli}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
			printf '%s' "${from_cli}"
			return 0
		fi
		warn "could not parse \`pi --version\` output (${from_cli:-empty}); falling back to npm global root"
	fi

	local npm_root
	npm_root="$(npm root -g 2>/dev/null || true)"
	[[ -n "${npm_root}" ]] || return 1
	read_manifest_version "${npm_root}/${SDK_MANIFEST_REL}"
}

# read_manifest_version prints a package.json's "version" field, or nothing
# when the file is missing or unparseable.
read_manifest_version() {
	local manifest="$1"
	[[ -f "${manifest}" ]] || return 1
	node -e '
		const fs = require("node:fs");
		try {
			const version = JSON.parse(fs.readFileSync(process.argv[1], "utf8")).version;
			process.stdout.write(typeof version === "string" ? version : "");
		} catch { process.stdout.write(""); }
	' "${manifest}"
}

# dependency_version prints the version bridge/package.json pins for one
# dependency ("" when absent). The bridge pins the SDK, not itself, so the
# package's own "version" field is never the PI SDK version.
dependency_version() {
	node -e '
		const fs = require("node:fs");
		try {
			const deps = JSON.parse(fs.readFileSync(process.argv[1], "utf8")).dependencies ?? {};
			process.stdout.write(deps[process.argv[2]] ?? "");
		} catch { process.stdout.write(""); }
	' "${REPO_MANIFEST}" "$1"
}

current_pin() {
	dependency_version "@earendil-works/pi-coding-agent"
}

go_pin() {
	sed -n 's/^const piSDKVersion = "\(.*\)"$/\1/p' "${GO_PIN_FILE}"
}

# write_pin updates both version declarations in a single pass so the two can
# only ever move together (TestPiSDKVersionMatchesSourceManifest enforces it).
write_pin() {
	local next="$1"
	[[ "${next}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "refusing to write non-semver version '${next}'"

	node -e '
		const fs = require("node:fs");
		const [file, version, ...packages] = process.argv.slice(1);
		const manifest = JSON.parse(fs.readFileSync(file, "utf8"));
		for (const name of packages) {
			if (!manifest.dependencies?.[name]) throw new Error(`bridge/package.json has no dependency ${name}`);
			manifest.dependencies[name] = version;
		}
		fs.writeFileSync(file, `${JSON.stringify(manifest, null, 2)}\n`);
	' "${REPO_MANIFEST}" "${next}" "${PI_PACKAGES[@]}"

	sed -i.bak -E "s|^(const piSDKVersion = \").*(\")$|\1${next}\2|" "${GO_PIN_FILE}"
	rm -f "${GO_PIN_FILE}.bak"

	[[ "$(go_pin)" == "${next}" ]] || die "failed to update piSDKVersion in ${GO_PIN_FILE}"
	[[ "$(current_pin)" == "${next}" ]] || die "failed to update bridge/package.json"
}

# validate runs every gate that a bump must pass before it can be deployed.
validate() {
	info "Installing bridge dependencies (npm install)"
	(cd "${BRIDGE_DIR}" && npm install)

	info "Typechecking the bridge against the installed SDK"
	(cd "${BRIDGE_DIR}" && npx tsc --noEmit)

	info "Running bridge tests (protocol, policy, telemetry, SDK surface)"
	(cd "${BRIDGE_DIR}" && npm test)

	info "Rebuilding the bundle and syncing embedded copies"
	(cd "${REPO_ROOT}" && make bridge)

	info "Running Go bridge tests"
	(cd "${REPO_ROOT}" && go test ./internal/bridge/... -short)
}

report_daemon() {
	local pinned="$1" installed
	installed="$(read_manifest_version "${DAEMON_SDK_MANIFEST}" || true)"
	if [[ -z "${installed}" ]]; then
		printf 'daemon tree: absent (installed on first daemon start)\n'
	elif [[ "${installed}" == "${pinned}" ]]; then
		printf 'daemon tree: %s (in sync)\n' "${installed}"
	else
		printf 'daemon tree: %s -> %s on next daemon start (npm reinstall)\n' "${installed}" "${pinned}"
	fi
}

main() {
	local target="" check_only=false force=false
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--version) [[ -n "${2:-}" ]] || die "--version needs a value"; target="$2"; shift 2 ;;
		--check) check_only=true; shift ;;
		--force) force=true; shift ;;
		-h | --help) usage; exit 0 ;;
		*) die "unknown argument '$1'" ;;
		esac
	done

	local pinned cli
	pinned="$(current_pin)"
	[[ -n "${pinned}" ]] || die "cannot read the pinned version from ${REPO_MANIFEST}"
	[[ "$(go_pin)" == "${pinned}" ]] || die "pi_sdk.go ($(go_pin)) and bridge/package.json (${pinned}) disagree; run with --version ${pinned} or fix by hand"
	for pkg in "${PI_PACKAGES[@]}"; do
		[[ "$(dependency_version "${pkg}")" == "${pinned}" ]] || die "${pkg} is not pinned to ${pinned} in ${REPO_MANIFEST}"
	done

	cli="${target}"
	if [[ -z "${cli}" ]]; then
		cli="$(detect_cli_version || true)"
		if [[ -z "${cli}" ]]; then
			[[ "${check_only}" == true ]] && { warn "PI CLI version unavailable — drift check skipped"; exit 0; }
			die "cannot determine the installed PI CLI version; pass --version X.Y.Z"
		fi
	fi

	info "pinned=${pinned} pi-cli=${cli}"
	report_daemon "${pinned}"

	if [[ "${check_only}" == true ]]; then
		[[ "${pinned}" == "${cli}" ]] || { warn "bridge SDK pin (${pinned}) differs from the installed PI CLI (${cli}); run scripts/sync-pi-sdk.sh"; exit 1; }
		info "in sync"
		return 0
	fi

	if [[ "${pinned}" == "${cli}" ]]; then
		if [[ "${force}" == false ]]; then
			info "already pinned to ${cli} — nothing to do (--force re-runs validation)"
			return 0
		fi
		info "re-validating at ${cli} (--force)"
		validate
		return 0
	fi

	info "Writing PI SDK ${pinned} -> ${cli}"
	write_pin "${cli}"
	validate

	cat <<EOF

==> PI SDK ${cli} is pinned, installed and verified.
    Files changed:
      bridge/package.json (+ package-lock.json)
      internal/bridge/pi_sdk.go
      bridge/bundle.js, internal/bridge/bundle.ts, internal/bridge/bundle.js (if the source changed)

    Still manual (needs review/approval):
      1. docs/pi-sdk-api-validation.md — refresh the validated version + evidence
      2. CHANGELOG.md and internal/version/version.go — version bump
      3. commit on a feature/* branch, then make deploy
      4. live validation: Telegram message, /model, a tool call, a session resume

    If this bump changed the package template (deps/engines/build script), also run
    the daemon install probe before deploying:
      AURELIA_BRIDGE_INSTALL_PROBE=1 go test ./internal/bridge/ \
        -run TestEnsureBridgeInstallProbe -v -timeout 15m

    On the first daemon start after deploy, EnsureBridge reinstalls node_modules
    at ${cli} and rebuilds the bundle from source (~30s before Telegram answers).
EOF
}

main "$@"
