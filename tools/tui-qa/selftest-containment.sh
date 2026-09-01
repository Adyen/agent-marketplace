#!/bin/sh
set -eu

fail=0
run_id="${AM_TUI_QA_RUN_ID:-containment}"
case "$run_id" in
  *[!A-Za-z0-9._-]*|'') echo "invalid containment run id" >&2; exit 2 ;;
esac
probe_id="$run_id-$$"

check_write_denied() {
  target="$1"
  if (printf x > "$target") 2>/dev/null; then
    echo "CONTAINMENT VIOLATION: wrote to $target" >&2
    fail=1
  else
    echo "ok: write denied at $target"
  fi
}

for required in /src /opt/agent-marketplace /qa/artifacts; do
  if [ ! -e "$required" ]; then
    echo "CONTAINMENT VIOLATION: required probe path missing: $required" >&2
    fail=1
  fi
done

check_write_denied "/etc/containment-probe-$probe_id"
check_write_denied "/usr/local/bin/containment-probe-$probe_id"
check_write_denied "/src/containment-probe-$probe_id"
check_write_denied "/opt/agent-marketplace"

artifact_probe="/qa/artifacts/containment-probe-$probe_id"
if (printf x > "$artifact_probe") 2>/dev/null; then
  echo "ok: writable QA artifact mount"
else
  echo "CONTAINMENT VIOLATION: /qa/artifacts not writable" >&2
  fail=1
fi

escape_link="${TMPDIR:-/tmp}/containment-escape-$probe_id"
ln -s /src "$escape_link"
check_write_denied "$escape_link/containment-probe"

if ! command -v curl >/dev/null 2>&1; then
  echo "CONTAINMENT VIOLATION: curl missing; network denial cannot be tested" >&2
  fail=1
elif curl -fsS --max-time 2 https://example.com >/dev/null 2>&1; then
  echo "CONTAINMENT VIOLATION: runtime network reachable" >&2
  fail=1
else
  echo "ok: runtime network denied"
fi

if [ "$fail" -ne 0 ]; then
  echo "containment self-test FAILED" >&2
  exit 1
fi
echo "containment self-test passed"
