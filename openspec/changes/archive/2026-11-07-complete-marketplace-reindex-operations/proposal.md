# Proposal

## Why

The Marketplace worker now observes sources, applies runtime configuration changes, and coordinates work across replicas, but operators can see that behavior only by reading logs, and a `--role worker` process exposes no metrics at all. Operators cannot tell a healthy quiet registry from a failing source, a failed refresh, a rejected configuration edit, or work delayed by a host budget without parsing log text.

## What Changes

- `serve --role worker` opens its operations listener (`operations_listen`) and serves `/metrics` there. `serve --role all` serves the same worker series on its existing operations `/metrics`, once. No new route or authentication path is added.
- The worker exports counters and histograms for source observations by outcome, registry refreshes by trigger and outcome, host-budget delays by operation, and configuration candidates by outcome, plus a restart-required gauge and per-registry gauges for the last successful observation, the last successful index, and consecutive probe failures. Label values come from fixed sets and operator-configured registry IDs only; origins, hosts, commits, generations, error text, job IDs, and credentials never become labels or appear in exported values. Series for a registry removed from the runtime configuration are dropped.
- Each finished refresh logs one `refresh finished` record with its registry, trigger, outcome, and duration, so a refresh failure is distinguishable from a source failure in the logs.
- The Helm chart exposes the operations port on the worker Deployment and its ServiceMonitor scrapes worker pods as well as API pods.
- Tests prove configuration replacement under a real `serve` process, both applied and rejected, and that two worker processes sharing one metadata store neither multiply probes nor run equivalent refreshes twice.
- Benchmarks with a 200-registry fixture measure the observation cycle, configuration classification, and due-observation selection before and after the instrumentation.
- Operator documentation explains how to read each signal.

## Capabilities

### New Capabilities

- `marketplace-worker-telemetry`: worker metrics exposure on the operations listener, the signals that distinguish healthy, failing, rejected, and delayed work, and the cardinality and secrecy bounds on those signals.

### Modified Capabilities

- `marketplace-runtime-configuration`: a running `serve` process reports whether a replaced configuration was applied or rejected.
- `marketplace-source-observation`: separate worker processes sharing one metadata store do not multiply probes or equivalent refreshes.

## Impact

- `serve` wiring for the worker role, the worker's observation and refresh processing, the runtime configuration source, the operations metrics registry, and the Helm chart's worker Deployment and ServiceMonitor.
- No metadata-store schema or interface change, no new operator configuration field, and no change to the public listener, API authentication, or revocation loading.
- A worker-only process now binds `operations_listen`; deployments that run a worker and an API in one network namespace with the same `operations_listen` must give them different addresses.

## Release note

The Marketplace worker now publishes Prometheus metrics for source observations, refreshes, host-budget delays, and configuration reloads, and `serve --role worker` serves them on its operations listener. A worker-only process now binds `operations_listen`, so a worker and an API sharing one network namespace need different operations addresses. The Helm chart scrapes worker pods when `serviceMonitor.enabled` is set.
