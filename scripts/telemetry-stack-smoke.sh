#!/bin/sh
set -eu

project="${COMPOSE_PROJECT_NAME:-agent-marketplace-telemetry-smoke-$$}"
compose_file="${COMPOSE_FILE:-compose.telemetry.yaml}"
export TELEMETRY_RECEIVER_PORT="${TELEMETRY_RECEIVER_PORT:-0}"
export TELEMETRY_PROMETHEUS_PORT="${TELEMETRY_PROMETHEUS_PORT:-0}"
export TELEMETRY_GRAFANA_PORT="${TELEMETRY_GRAFANA_PORT:-0}"

dc() {
	docker compose -p "$project" -f "$compose_file" "$@"
}

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		dc ps >&2 || true
		dc logs --no-color >&2 || true
	fi
	dc down --volumes --remove-orphans >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT INT TERM

wait_for() {
	name=$1
	url=$2
	needle=$3
	attempt=0
	while [ "$attempt" -lt 90 ]; do
		body=$(dc exec -T receiver wget -qO- "$url" 2>/dev/null || true)
		if printf '%s' "$body" | grep -F "$needle" >/dev/null 2>&1; then
			printf 'ok: %s\n' "$name"
			return 0
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	printf 'timed out waiting for %s: %s\n' "$name" "$url" >&2
	return 1
}

dc up --build -d receiver prometheus grafana
wait_for "receiver health" "http://receiver:4318/health" '"status":"ok"'
wait_for "Prometheus readiness" "http://prometheus:9090/-/ready" "Prometheus Server is Ready"
wait_for "receiver scrape target" "http://prometheus:9090/api/v1/targets" '"health":"up"'
wait_for "Grafana health" "http://grafana:3000/api/health" '"database":"ok"'
wait_for "provisioned datasource" "http://grafana:3000/api/datasources/uid/prometheus" '"uid":"prometheus"'
wait_for "provisioned dashboard" "http://grafana:3000/api/dashboards/uid/agent-marketplace-telemetry" '"title":"Agent Marketplace Telemetry"'

dc run --rm smoke-event >/dev/null
wait_for "receiver first invocation" "http://receiver:4318/metrics" 'command_name="agent-marketplace doctor"'
wait_for "receiver CLI version" "http://receiver:4318/metrics" 'cli_version="compose-smoke"'
wait_for "Prometheus first invocation" "http://prometheus:9090/api/v1/query?query=sum%28agent_marketplace_receiver_invocations_total%29" ',"1"]'
wait_for "Grafana first invocation" "http://grafana:3000/api/datasources/proxy/uid/prometheus/api/v1/query?query=sum%28agent_marketplace_receiver_invocations_total%7Bcommand_name%3D%22agent-marketplace%20doctor%22%7D%29" ',"1"]'

prometheus_address=$(dc port prometheus 9090)
PROMETHEUS_URL="http://$prometheus_address" go test ./monitoring -run TestDashboardPromQL -count=1
printf 'telemetry stack smoke check passed\n'
