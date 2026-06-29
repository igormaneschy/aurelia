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
cat >"${ENV_FILE}" <<'EOF'
# Aurelia CLI — added by scripts/install-path.sh (make install-path)
export PATH="$HOME/.aurelia/bin:$PATH"
EOF
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
		printf '%s\n' 'fish_add_path --prepend $HOME/.aurelia/bin' >"${fish_conf}"
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