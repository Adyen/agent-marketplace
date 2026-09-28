#!/usr/bin/env bash
set -euo pipefail

package_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$package_root/../.." && pwd)
output_dir=${AM_TUI_RECORDING_DIR:-"$package_root/artifacts/tui-test-recording-poc"}
driver_root=$(mktemp -d /tmp/am-tui-recording.XXXXXX)
driver_home="$driver_root/home"
fixture_json="$driver_root/fixture.json"
session=am-recording-poc
version=0.1.0-beta.4
fixture_root=
fixture_validated=false
tt=

cleanup() {
  if [[ -n "$tt" ]]; then
    HOME="$driver_home" "$tt" record stop --session "$session" >/dev/null 2>&1 || true
    HOME="$driver_home" "$tt" close --session "$session" >/dev/null 2>&1 || true
    HOME="$driver_home" "$tt" daemon stop --session "$session" >/dev/null 2>&1 || true
  fi
  if [[ "$fixture_validated" == true ]]; then
    rm -rf -- "$fixture_root"
  fi
  rm -rf -- "$driver_root"
}
trap cleanup EXIT

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)
    asset=tui-test-aarch64-apple-darwin.tar.gz
    checksum=dd105ad15813aa7391c8cbb17ccd895d965f2914a057b57c1f4f49b419229160
    ;;
  Darwin-x86_64)
    asset=tui-test-x86_64-apple-darwin.tar.gz
    checksum=181d72de564f29b60482607d52f4d6c0e97b3b3102844984168cf112bc30984a
    ;;
  Linux-aarch64 | Linux-arm64)
    asset=tui-test-aarch64-unknown-linux-gnu.tar.gz
    checksum=d93e06aaa60c7e5a1b7dab3f15dbb06f484d95ffdb6b2cc44ffb9f08f556f8da
    ;;
  Linux-x86_64)
    asset=tui-test-x86_64-unknown-linux-gnu.tar.gz
    checksum=18579937967cc1f5880a4ae1c89fe98ea5eec5c0e6448259cfc5da83880120f3
    ;;
  *)
    echo "unsupported recording platform: $(uname -s)-$(uname -m)" >&2
    exit 1
    ;;
esac

mkdir -p "$driver_home" "$output_dir"
archive="$driver_root/$asset"
curl --fail --location --retry 3 \
  "https://github.com/microsoft/tui-test/releases/download/$version/$asset" \
  --output "$archive"
if command -v sha256sum >/dev/null; then
  printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check -
else
  printf '%s  %s\n' "$checksum" "$archive" | shasum -a 256 --check -
fi
while IFS= read -r member; do
  case "$member" in
    "" | /* | .. | ../* | */.. | */../*)
      echo "unsafe path in tui-test release: $member" >&2
      exit 1
      ;;
  esac
done < <(tar -tzf "$archive")
if tar -tvzf "$archive" | grep -Ev '^[-d]' >/dev/null; then
  echo "unsupported link or special file in tui-test release" >&2
  exit 1
fi
tar -xzf "$archive" -C "$driver_root"
tt="$driver_root/tui-test"
if [[ ! -f "$tt" || -L "$tt" || ! -x "$tt" ]]; then
  echo "tui-test release did not contain a regular executable" >&2
  exit 1
fi
"$tt" --version

binary=${AM_TUI_QA_BINARY:-"$driver_root/agent-marketplace-tui-qa"}
if [[ ! -x "$binary" ]]; then
  (
    cd "$repository_root"
    CGO_ENABLED=0 go build -tags e2e \
      -ldflags "-X main.version=tui-recording-poc" \
      -o "$binary" \
      ./cmd/agent-marketplace
  )
fi

(
  cd "$package_root"
  pnpm build
  AM_TUI_QA_REPO_ROOT="$repository_root" \
    AM_TUI_QA_BINARY="$binary" \
    AM_TUI_RECORDING_FIXTURE="$fixture_json" \
    node dist/tests/recording-fixture.js
)

fixture_root=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).root)' "$fixture_json")
wrapper=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).wrapper)' "$fixture_json")
fixture_tmp=$(node -e 'console.log(require("os").tmpdir())')
if [[ "$fixture_root" != "$fixture_tmp"/agent-marketplace-tui-recording-poc-* ]]; then
  echo "recording fixture escaped the temporary root: $fixture_root" >&2
  exit 1
fi
if [[ "$wrapper" != "$driver_root/launch-recording-fixture" ]]; then
  echo "recording launcher escaped the driver root: $wrapper" >&2
  exit 1
fi
fixture_validated=true

run_tt() {
  HOME="$driver_home" \
    XDG_CONFIG_HOME="$driver_root/xdg/config" \
    XDG_CACHE_HOME="$driver_root/xdg/cache" \
    XDG_STATE_HOME="$driver_root/xdg/state" \
    "$tt" "$@"
}

gif="$output_dir/browse-inspect.gif"
png="$output_dir/browse-inspect-final.png"
rm -f -- "$gif" "$png"

run_tt run --session "$session" --backend xtermjs --cols 100 --rows 30 --restart "$wrapper"
run_tt expect text --session "$session" --timeout 30000 'Configuration migration required'
run_tt type --session "$session" l
run_tt expect text --session "$session" --timeout 5000 'Config left unchanged'
run_tt expect text --session "$session" --timeout 30000 beta-skill
run_tt expect text --session "$session" --timeout 30000 alpha-skill
run_tt expect text --session "$session" --timeout 30000 --whitespace normalize '~ in sync'
run_tt record start --session "$session" --fps 12 --idle-time-limit 1.5 \
  --zoom 0.75 --background '#0b1020' "$gif"

sleep 1
run_tt type --session "$session" 2
run_tt expect text --session "$session" --regex --timeout 5000 'fixture / skills[[:space:]]+\(2\)'
sleep 0.8
run_tt type --session "$session" 3
run_tt expect text --session "$session" --regex --timeout 5000 'Enabled[[:space:]]+\(1\)'
run_tt expect text --session "$session" --regex --timeout 5000 'Available[[:space:]]+\(1\)'
sleep 0.8
run_tt type --session "$session" 1
run_tt expect text --session "$session" --regex --timeout 5000 'Project[[:space:]]+\(1\)'
sleep 0.8
run_tt type --session "$session" G
run_tt expect text --session "$session" --regex --timeout 5000 'Name:[[:space:]]+alpha-skill'
run_tt expect text --session "$session" --regex --timeout 5000 'Status:[[:space:]]+available'
sleep 1
run_tt key press --session "$session" Enter
run_tt expect text --session "$session" --timeout 5000 'Alpha fixture body.'
sleep 1.2
run_tt key press --session "$session" Escape
run_tt expect text --session "$session" --regex --timeout 5000 \
  '1 Scope[[:space:]]+2 Registry[[:space:]]+3 Status'
run_tt expect text --session "$session" --regex --timeout 5000 'Name:[[:space:]]+alpha-skill'
run_tt screenshot --session "$session" --zoom 0.75 --background '#0b1020' "$png"
sleep 1
run_tt record stop --session "$session"
run_tt type --session "$session" q
run_tt wait exit --session "$session" --timeout 5000
run_tt close --session "$session"

ls -lh "$gif" "$png"
