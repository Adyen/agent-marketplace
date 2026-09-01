#!/bin/sh
set -eu

root_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
run_id="${AM_TUI_QA_RUN_ID:-tui-qa-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
out_dir="${AM_TUI_QA_CONTAINER_OUT:-$root_dir/.local/agent-marketplace/qa/tui/$run_id}"
image="${TUI_QA_IMAGE:-agent-marketplace-tui-qa:local}"
uid=$(id -u)
gid=$(id -g)

case "$run_id" in
  *[!A-Za-z0-9._-]*|'') echo "invalid AM_TUI_QA_RUN_ID" >&2; exit 3 ;;
esac

mkdir -p "$(dirname -- "$out_dir")"
if ! mkdir "$out_dir"; then
  echo "refusing to reuse TUI QA output directory: $out_dir" >&2
  exit 3
fi
mkdir "$out_dir/bin" "$out_dir/artifacts"

build_image() {
  if [ -f /usr/local/adyen/etc/openssl/cert.pem ]; then
    docker build "$@" --secret "id=organization-ca,src=/usr/local/adyen/etc/openssl/cert.pem" -t "$image" -f "$root_dir/tools/tui-qa/Dockerfile" "$root_dir"
  else
    docker build "$@" -t "$image" -f "$root_dir/tools/tui-qa/Dockerfile" "$root_dir"
  fi
}

if [ -n "${TUI_QA_BASE_IMAGE:-}" ]; then
  build_image --build-arg "BASE_IMAGE=$TUI_QA_BASE_IMAGE" --build-arg "BASE_FLAVOR=${TUI_QA_BASE_FLAVOR:-oracle}"
else
  build_image
fi

image_arch=$(docker image inspect --format '{{.Architecture}}' "$image")
case "$image_arch" in
  amd64|arm64) goarch="$image_arch" ;;
  *) echo "unsupported container image architecture: $image_arch" >&2; exit 3 ;;
esac

(cd "$root_dir/tools/tui-qa" && npm run build)
GOOS=linux GOARCH="$goarch" \
AM_TUI_QA_REPO_ROOT="$root_dir" \
AM_TUI_QA_BUILD_OUT="$out_dir/bin/agent-marketplace" \
AM_TUI_QA_BUILD_MANIFEST="$out_dir/bin/build-identity.json" \
node "$root_dir/tools/tui-qa/dist/build-release.js"
chmod 0555 "$out_dir/bin/agent-marketplace"
chmod 0444 "$out_dir/bin/build-identity.json"
head_sha=$(git -C "$root_dir" rev-parse HEAD)
base_sha=$(git -C "$root_dir" merge-base HEAD origin/main 2>/dev/null || printf NO_BASE)
if [ -n "$(git -C "$root_dir" status --porcelain)" ]; then worktree_dirty=1; else worktree_dirty=0; fi

docker run --rm \
  --name "agent-marketplace-tui-qa-$run_id" \
  --user "$uid:$gid" \
  --init \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 512 \
  --network none \
  --read-only \
  --tmpfs "/sandbox:rw,nosuid,nodev,exec,size=1g,uid=$uid,gid=$gid,mode=0700" \
  --tmpfs "/workbench/.tui-test:rw,nosuid,nodev,noexec,size=128m,uid=$uid,gid=$gid,mode=0700" \
  -v "$root_dir:/src:ro" \
  -v "$out_dir/artifacts:/qa/artifacts:rw" \
  -v "$out_dir/bin/agent-marketplace:/opt/agent-marketplace:ro" \
  -v "$out_dir/bin/build-identity.json:/opt/build-identity.json:ro" \
  -e AM_TUI_QA_RUN_ID="$run_id" \
  -e AM_TUI_QA_RUN_CLASS="${AM_TUI_QA_RUN_CLASS:-baseline}" \
  -e AM_TUI_QA_RETRIES="${AM_TUI_QA_RETRIES:-1}" \
  -e AM_TUI_QA_ARTIFACT_DIR=/qa/artifacts \
  -e AM_TUI_QA_BINARY=/opt/agent-marketplace \
  -e AM_TUI_QA_BUILD_IDENTITY=/opt/build-identity.json \
  -e AM_TUI_QA_REPO_ROOT=/src \
  -e AM_TUI_QA_HEAD_SHA="$head_sha" \
  -e AM_TUI_QA_BASE_SHA="$base_sha" \
  -e AM_TUI_QA_WORKTREE_DIRTY="$worktree_dirty" \
  "$image"

printf 'TUI QA container disposed; artifacts: %s\n' "$out_dir/artifacts"
