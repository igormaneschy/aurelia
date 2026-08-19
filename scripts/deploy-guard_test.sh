#!/bin/sh
set -eu

script=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/deploy-guard.sh
tmp=$(mktemp -d "${TMPDIR:-/tmp}/deploy-guard-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
expect_fail() {
	name=$1; shift
	if "$@" >"$tmp/stdout" 2>"$tmp/stderr"; then fail "$name unexpectedly succeeded"; fi
}
make_fake() {
	name=$1; output=$2; mode=${3:-ok}
	path="$tmp/$name"
	case "$mode" in
	missing) return ;;
	noexec) printf '%s\n' '#!/bin/sh' >"$path"; return ;;
	*)
		cat >"$path" <<EOF
#!/bin/sh
$(if [ "$mode" = fail ]; then printf 'exit 7'; else printf 'printf '\''%s'\'' '\''%s'\''\n' "$output"; fi)
EOF
		chmod +x "$path"
	;;
	esac
}

expect_fail missing-binary "$script" "$tmp/missing" 0
make_fake noexec '{"RunsRunning":0}' noexec
expect_fail non-executable "$script" "$tmp/noexec" 0
make_fake metrics-fail '' fail
expect_fail metrics-command-failure "$script" "$tmp/metrics-fail" 0
for name in malformed missing nonnumeric negative; do
	case "$name" in
	malformed) json='{not json' ;;
	missing) json='{"Other":0}' ;;
	nonnumeric) json='{"RunsRunning":"0"}' ;;
	negative) json='{"RunsRunning":-1}' ;;
	esac
	make_fake "$name" "$json"
	expect_fail "$name-metrics" "$script" "$tmp/$name" 0
done

make_fake zero '{"RunsRunning":0}'
"$script" "$tmp/zero" 0 >"$tmp/stdout" || fail zero-runs
grep -q 'safe to restart' "$tmp/stdout" || fail zero-runs-message

cat >"$tmp/active" <<'EOF'
#!/bin/sh
count_file="$TMPDIR/deploy-guard-count"
count=$(cat "$count_file" 2>/dev/null || printf 0)
count=$((count + 1)); printf '%s\n' "$count" >"$count_file"
if [ "$count" -eq 1 ]; then printf '%s\n' '{"RunsRunning":1}'; else printf '%s\n' '{"RunsRunning":0}'; fi
EOF
chmod +x "$tmp/active"
TMPDIR="$tmp" "$script" "$tmp/active" 2 >"$tmp/stdout" || fail active-run-drain
grep -q 'safe to restart' "$tmp/stdout" || fail active-run-drain-message

make_fake active-abort '{"RunsRunning":1}'
expect_fail active-run-abort "$script" "$tmp/active-abort" 0
grep -q 'ABORTING kickstart' "$tmp/stderr" || fail active-run-abort-message

# A failing `date` must abort (an empty `now` would otherwise loop forever).
mkdir -p "$tmp/bin-date-fail"
cat >"$tmp/bin-date-fail/date" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$tmp/bin-date-fail/date"
make_fake any-runs '{"RunsRunning":0}'
if PATH="$tmp/bin-date-fail:$PATH" "$script" "$tmp/any-runs" 0 >"$tmp/stdout" 2>"$tmp/stderr"; then
	fail "date-failure unexpectedly succeeded"
fi
grep -q 'date failed' "$tmp/stderr" || fail date-failure-message

# A failing `sleep` during drain must abort instead of looping silently.
mkdir -p "$tmp/bin-sleep-fail"
cat >"$tmp/bin-sleep-fail/sleep" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$tmp/bin-sleep-fail/sleep"
make_fake always-active '{"RunsRunning":1}'
if PATH="$tmp/bin-sleep-fail:$PATH" "$script" "$tmp/always-active" 5 >"$tmp/stdout" 2>"$tmp/stderr"; then
	fail "sleep-failure unexpectedly succeeded"
fi
grep -q 'sleep failed' "$tmp/stderr" || fail sleep-failure-message

FORCE=1 "$script" "$tmp/missing" 0 >"$tmp/stdout" || fail force-bypass
grep -q 'FORCE=1' "$tmp/stdout" || fail force-message

printf 'PASS: deploy-guard deterministic failure and success branches\n'
