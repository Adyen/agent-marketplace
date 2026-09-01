#!/bin/sh
set -eu

export AM_TUI_QA_CONTAINED=1
export AM_TUI_QA_RUN_CLASS="${AM_TUI_QA_RUN_CLASS:-baseline}"
mkdir -p /sandbox/home /sandbox/go-build /sandbox/go-mod /sandbox/go-path /sandbox/npm-cache /qa/artifacts

if /workbench/selftest-containment.sh > /qa/artifacts/containment-selftest.txt 2>&1; then
  cat /qa/artifacts/containment-selftest.txt
else
  cat /qa/artifacts/containment-selftest.txt >&2
  exit 2
fi

cd /workbench
exec node dist/run-workbench.js
