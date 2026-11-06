#!/usr/bin/env bash
set -euo pipefail

root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
app_pid=
stub_pid=
control=
control_ready=
cleanup() {
  if [[ $control_ready == yes ]]; then
    curl -fsS -X POST "$control/__e2e/quit" >/dev/null 2>&1 || true
  fi
  if [[ -n "$app_pid" ]]; then
    kill "$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
  fi
  if [[ -n "$stub_pid" ]]; then
    kill "$stub_pid" 2>/dev/null || true
    wait "$stub_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

dist="$work/dist"
make -C "$root" VERSION=v0.0.1 DIST_DIR="$dist" GUI_BUILD_TAGS=desktop,production,e2e gui-package-macos
installed="$work/installed/Agent Marketplace.app"
mkdir -p "$(dirname "$installed")"
ditto "$dist/gui/macos/Agent Marketplace.app" "$installed"
make -C "$root" VERSION=v0.0.2 DIST_DIR="$dist" GUI_BUILD_TAGS=desktop,production,e2e gui-package-macos

zip_name=agent-marketplace-gui-v0.0.2-macos-universal.zip
stub="$work/stub"
download="$stub/Adyen/agent-marketplace/releases/download/v0.0.2"
mkdir -p "$download" "$stub/repos/Adyen/agent-marketplace/releases"
cp -f "$dist/gui/$zip_name" "$download/$zip_name"
(
  cd "$download"
  shasum -a 256 "$zip_name" > SHA256SUMS
)
control_port=47623
release_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
while [[ $release_port == "$control_port" ]]; do
  release_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
done
python3 - "$stub" "$release_port" "$zip_name" <<'PY'
import json
import pathlib
import sys

root, port, asset = sys.argv[1:]
base = f"http://127.0.0.1:{port}/Adyen/agent-marketplace/releases"
latest = {
    "tag_name": "v0.0.2",
    "draft": False,
    "prerelease": False,
    "published_at": "2026-01-01T00:00:00Z",
    "html_url": f"{base}/tag/v0.0.2",
    "assets": [
        {"name": asset, "browser_download_url": f"{base}/download/v0.0.2/{asset}"},
        {"name": "SHA256SUMS", "browser_download_url": f"{base}/download/v0.0.2/SHA256SUMS"},
    ],
}
pathlib.Path(root, "repos/Adyen/agent-marketplace/releases/latest").write_text(json.dumps(latest))
PY
python3 -m http.server "$release_port" --bind 127.0.0.1 --directory "$stub" > "$work/stub.log" 2>&1 &
stub_pid=$!

control=http://127.0.0.1:$control_port
home="$work/home"
mkdir -p "$home"
release_ready=
for _ in $(seq 1 60); do
  if curl -fsS "$control/__e2e/bootstrap" >/dev/null 2>&1; then
    echo "E2E control port is already in use" >&2
    exit 1
  fi
  if curl -fsS "http://127.0.0.1:$release_port/repos/Adyen/agent-marketplace/releases/latest" >/dev/null 2>&1; then
    release_ready=yes
    break
  fi
  sleep 0.5
done
test "$release_ready" = yes
AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT="http://127.0.0.1:$release_port/repos/Adyen/agent-marketplace/releases/latest" \
AGENT_MARKETPLACE_E2E_CONTROL_ADDR="127.0.0.1:$control_port" \
AGENT_MARKETPLACE_NO_UPDATE_CHECK= CI= HOME="$home" \
XDG_CONFIG_HOME="$home/.config" XDG_STATE_HOME="$home/.local/state" XDG_DATA_HOME="$home/.local/share" \
"$installed/Contents/MacOS/agent-marketplace-gui" > "$work/gui.log" 2>&1 &
app_pid=$!

bootstrap=
for _ in $(seq 1 120); do
  bootstrap=$(curl -fsS "$control/__e2e/bootstrap" 2>/dev/null || true)
  if [[ -n "$bootstrap" ]]; then
    break
  fi
  if ! kill -0 "$app_pid" 2>/dev/null; then
    cat "$work/gui.log" >&2
    exit 1
  fi
  sleep 0.5
done
test -n "$bootstrap"
control_ready=yes
credential=$(printf '%s' "$bootstrap" | jq -r .credential)
test "$(printf '%s' "$bootstrap" | jq -r .version)" = v0.0.1

api() {
  curl -fsS -H "X-Agent-Marketplace-Token: $credential" -H 'Content-Type: application/json' \
    -X POST -d "$1" "$control/api/$2"
}
release=$(curl -fsS -H "X-Agent-Marketplace-Token: $credential" -X POST "$control/api/release")
test "$(printf '%s' "$release" | jq -r .latestVersion)" = v0.0.2
api '{"action":"start"}' update >/dev/null
status=
for _ in $(seq 1 180); do
  status=$(api '{"action":"status"}' update)
  phase=$(printf '%s' "$status" | jq -r .phase)
  if [[ $phase == ready ]]; then
    break
  fi
  if [[ $phase == failed ]]; then
    printf '%s\\n' "$status" >&2
    exit 1
  fi
  sleep 1
done
test "$(printf '%s' "$status" | jq -r .phase)" = ready
confirmed=$(api '{"action":"confirm"}' update)
test "$(printf '%s' "$confirmed" | jq -r .phase)" = complete
curl -fsS -X POST "$control/__e2e/quit" >/dev/null

for _ in $(seq 1 60); do
  if ! kill -0 "$app_pid" 2>/dev/null; then
    break
  fi
  sleep 0.5
done
if kill -0 "$app_pid" 2>/dev/null; then
  echo "updated app did not exit" >&2
  exit 1
fi
wait "$app_pid" || true
app_pid=
version=
for _ in $(seq 1 120); do
  bootstrap=$(curl -fsS "$control/__e2e/bootstrap" 2>/dev/null || true)
  version=$(printf '%s' "$bootstrap" | jq -r .version 2>/dev/null || true)
  if [[ $version == v0.0.2 ]]; then
    break
  fi
  sleep 0.5
done
test "$version" = v0.0.2
curl -fsS -X POST "$control/__e2e/quit" >/dev/null
