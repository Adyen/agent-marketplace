#!/bin/sh
set -eu

image="${TUI_QA_CONTAINER_IMAGE:-}"
if [ -z "$image" ]; then
  echo "TUI_QA_CONTAINER_IMAGE is required" >&2
  exit 2
fi

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/tui-qa-containment.XXXXXX")
trap 'rm -rf -- "$tmp_root"' EXIT HUP INT TERM
mkdir -p "$tmp_root/artifacts"
printf 'probe\n' > "$tmp_root/agent-marketplace"
chmod 0444 "$tmp_root/agent-marketplace"

docker run --rm --init \
  --user "$(id -u):$(id -g)" \
  --network none \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 64 \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=32m \
  -e AM_TUI_QA_RUN_ID=containment \
  -v "$root_dir:/src:ro" \
  -v "$tmp_root/artifacts:/qa/artifacts:rw" \
  -v "$tmp_root/agent-marketplace:/opt/agent-marketplace:ro" \
  --entrypoint /bin/sh \
  "$image" \
  /src/tools/tui-qa/selftest-containment.sh
