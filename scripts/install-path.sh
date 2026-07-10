#!/usr/bin/env bash
# install-path.sh — put ~/.aurelia/bin on PATH for interactive shells.
#
# Idempotent: safe to re-run. Writes ~/.aurelia/env and a single sourced
# block in the user's shell rc file(s).

set -euo pipefail

AURELIA_HOME="${HOME}/.aurelia"
BIN_DIR="${AURELIA_HOME}/bin"
ENV_FILE="${AURELIA_HOME}/env"

MARKER_BEGIN='# >>> aurelia path >>>'
MARKER_END='# <<< aurelia path <<<'
SOURCE_BLOCK="${MARKER_BEGIN}
[ -f \"\${HOME}/.aurelia/env\" ] && . \"\${HOME}/.aurelia/env\"
${MARKER_END}"

mkdir -p "${BIN_DIR}"
WRAPPER_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/aurelia-tui"
USER_BIN="${HOME}/bin"
if [[ -f "${WRAPPER_SRC}" ]]; then
	mkdir -p "${USER_BIN}"
	install -m 755 "${WRAPPER_SRC}" "${USER_BIN}/aurelia-tui"
fi

case "$(basename "${SHELL:-}")" in
	zsh)
		cat >"${ENV_FILE}" <<'EOF'
# Aurelia CLI — added by scripts/install-path.sh (make install-path)
export PATH="$HOME/.aurelia/bin:$PATH"

# Fixed TUI shortcuts (shadow the binary when this file is sourced).
_aurelia_tui_bin="${HOME}/.aurelia/bin/aurelia-tui"
aurelia-tui() {
	case "${1:-}" in
		configure)
			shift
			command "${_aurelia_tui_bin}" --session configure "$@"
			;;
		aurelia)
			shift
			command "${_aurelia_tui_bin}" "$@"
			;;
		*)
			command "${_aurelia_tui_bin}" "$@"
			;;
	esac
}
EOF
		;;
	bash)
		cat >"${ENV_FILE}" <<'EOF'
# Aurelia CLI — added by scripts/install-path.sh (make install-path)
export PATH="$HOME/.aurelia/bin:$PATH"

# Fixed TUI shortcuts (shadow the binary when this file is sourced).
_aurelia_tui_bin="${HOME}/.aurelia/bin/aurelia-tui"
aurelia-tui() {
	case "${1:-}" in
		configure)
			shift
			command "${_aurelia_tui_bin}" --session configure "$@"
			;;
		aurelia)
			shift
			command "${_aurelia_tui_bin}" "$@"
			;;
		*)
			command "${_aurelia_tui_bin}" "$@"
			;;
	esac
}
EOF
		;;
	fish)
		cat >"${ENV_FILE}" <<'EOF'
# Aurelia CLI — added by scripts/install-path.sh (make install-path)
# Fish users: shortcuts live in ~/.config/fish/conf.d/aurelia.fish
export PATH="$HOME/.aurelia/bin:$PATH"
EOF
		;;
	*)
		cat >"${ENV_FILE}" <<'EOF'
# Aurelia CLI — added by scripts/install-path.sh (make install-path)
export PATH="$HOME/.aurelia/bin:$PATH"
EOF
		;;
esac
chmod 644 "${ENV_FILE}"

append_block() {
	local rc="$1"
	[[ -f "${rc}" ]] || return 0
	if grep -qF "${MARKER_BEGIN}" "${rc}" 2>/dev/null; then
		return 0
	fi
	printf '\n%s\n' "${SOURCE_BLOCK}" >>"${rc}"
	echo "updated ${rc}"
}

created=()
case "$(basename "${SHELL:-}")" in
	zsh)
		created+=("${HOME}/.zshrc")
		[[ -f "${HOME}/.zshrc" ]] || touch "${HOME}/.zshrc"
		append_block "${HOME}/.zshrc"
		append_block "${HOME}/.zprofile"
		;;
	bash)
		created+=("${HOME}/.bash_profile")
		[[ -f "${HOME}/.bash_profile" ]] || touch "${HOME}/.bash_profile"
		append_block "${HOME}/.bash_profile"
		append_block "${HOME}/.bashrc"
		;;
	fish)
		mkdir -p "${HOME}/.config/fish/conf.d"
		fish_conf="${HOME}/.config/fish/conf.d/aurelia.fish"
		cat >"${fish_conf}" <<'EOF'
fish_add_path --prepend $HOME/.aurelia/bin

function aurelia-tui --wraps aurelia-tui
	set -l bin $HOME/.aurelia/bin/aurelia-tui
	switch $argv[1]
		case configure
			command $bin --session configure $argv[2..-1]
		case aurelia
			command $bin $argv[2..-1]
		case '*'
			command $bin $argv
	end
end
EOF
		echo "updated ${fish_conf}"
		;;
	*)
		[[ -f "${HOME}/.zshrc" ]] || touch "${HOME}/.zshrc"
		append_block "${HOME}/.zshrc"
		;;
esac

echo "Aurelia bin directory: ${BIN_DIR}"
echo "Run: source ${ENV_FILE}"
echo "Or open a new terminal, then: aurelia-tui"