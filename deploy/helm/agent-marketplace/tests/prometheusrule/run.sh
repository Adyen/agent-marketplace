#!/usr/bin/env bash
set -euo pipefail

test_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
chart_dir="$(cd -- "$test_dir/../.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

rendered="$tmp_dir/prometheusrule.yaml"
rules="$tmp_dir/rules.yaml"
helm template agent-marketplace "$chart_dir" \
  --namespace marketplace-system \
  --show-only templates/prometheusrule.yaml \
  --set image.tag=ci \
  --set prometheusRule.enabled=true >"$rendered"

awk '
  /^spec:$/ { in_spec = 1; next }
  in_spec && /^  / { sub(/^  /, ""); print; next }
  in_spec { exit }
' "$rendered" >"$rules"

if ! grep -q '^groups:' "$rules"; then
  printf 'PrometheusRule spec did not contain rule groups\n' >&2
  exit 1
fi

cp "$test_dir/rules_test.yaml" "$tmp_dir/rules_test.yaml"

read -r -a promtool_cmd <<<"${PROMTOOL:-promtool}"
"${promtool_cmd[@]}" check rules "$rules"
"${promtool_cmd[@]}" test rules "$tmp_dir/rules_test.yaml"
