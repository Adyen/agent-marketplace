#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off

# Setup (including downloading the release) is separate from native execution.
# This launcher never pulls an image or mounts the checkout, HOME, or credentials.
binary=${OPENCODE_PROBE_BINARY:?path to the verified v1.18.34 Linux arm64 executable required}
image=${OPENCODE_PROBE_IMAGE:-ubuntu:24.04}
root=$(cd "$(dirname "$0")/../.." && pwd)
binary=$(cd "$(dirname "$binary")" && pwd -P)/$(basename "$binary")
test -f "$binary" && test -r "$binary" && test -x "$binary"
image_id=$(docker image inspect "$image" --format '{{.Id}}')
test "$(docker image inspect "$image_id" --format '{{.Os}}/{{.Architecture}}')" = linux/arm64
attempt=$(mktemp -d "${TMPDIR:-/tmp}/opencode-probe.XXXXXX")
container=${attempt##*/}
runner_pid=
creator_pid=
creation_watchdog=
# shellcheck disable=SC2329 # Invoked by the EXIT trap.
cleanup() {
  trap '' INT TERM
  if [[ -n "$creator_pid" ]]; then
    wait "$creator_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$creation_watchdog" ]]; then
    kill -KILL -- "-$creation_watchdog" >/dev/null 2>&1 || true
    wait "$creation_watchdog" >/dev/null 2>&1 || true
  fi
  if [[ -n "$runner_pid" ]]; then
    kill -TERM -- "-$runner_pid" >/dev/null 2>&1 || true
  fi
  if [[ -s "$attempt/container.id" ]]; then
    docker rm -f "$(cat "$attempt/container.id")" >/dev/null 2>&1 || true
  fi
  if [[ -n "$runner_pid" ]]; then
    kill -KILL -- "-$runner_pid" >/dev/null 2>&1 || true
    wait "$runner_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$attempt"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cd "$root"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./tools/opencode-probe -o "$attempt/probe.test"

witnesses=TestNativeActivationControls,TestNativeRequiredActivationGate,TestNativeCompiledCommandHooks,TestNativeInstalledCommandHooks,TestNativeInstalledHookSupport,TestNativePermissionsAndPlugins,TestNativeInvocationBudget,TestNativeInstalledResourceDiscovery
# Creation gets its own group so terminal/group cancellation cannot interrupt
# CID capture. Cleanup waits for it before starting or removing a container.
creation_cancelled=0
# Do not exit between spawning a job and recording the PID cleanup must wait for.
trap 'creation_cancelled=130' INT
trap 'creation_cancelled=143' TERM
set -m
(
set +m
docker create --rm --init --name "$container" --cidfile "$attempt/container.id" --platform linux/arm64 \
  --network none --cap-drop ALL --security-opt no-new-privileges \
  --read-only --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
  --user 65534:65534 --pids-limit 128 --memory 2g --cpus 2 \
  --mount "type=bind,src=$binary,dst=/native/opencode,readonly" \
  --mount "type=bind,src=$attempt/probe.test,dst=/native/probe.test,readonly" \
  --env OPENCODE_PROBE_BINARY=/native/opencode --env OPENCODE_PROBE_REQUIRE=1 \
  "$image_id" /native/probe.test -test.v=test2json -test.timeout=10m \
  "-test.run=^(${witnesses//,/|})$" >/dev/null
) &
creator_pid=$!
(sleep 30; kill -KILL -- "-$creator_pid" >/dev/null 2>&1 || true) &
creation_watchdog=$!
trap 'exit 130' INT
trap 'exit 143' TERM
if [[ "$creation_cancelled" != 0 ]]; then exit "$creation_cancelled"; fi
creation_result=0
wait "$creator_pid" || creation_result=$?
creator_pid=
kill -KILL -- "-$creation_watchdog" >/dev/null 2>&1 || true
wait "$creation_watchdog" >/dev/null 2>&1 || true
creation_watchdog=
if [[ "$creation_result" != 0 ]]; then exit "$creation_result"; fi
# A background job has its own process group. Waiting with the shell builtin
# lets trapped cancellation immediately remove the container and reap the group.
(
set +m
go tool test2json -t -p opencode-probe \
  docker start --attach "$(cat "$attempt/container.id")" \
  | CGO_ENABLED=0 go run ./tools/witness-check -require "$witnesses"
) &
runner_pid=$!
result=0
wait "$runner_pid" || result=$?
runner_pid=
exit "$result"
