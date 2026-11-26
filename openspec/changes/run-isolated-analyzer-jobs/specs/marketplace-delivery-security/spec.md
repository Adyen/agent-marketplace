## ADDED Requirements

### Requirement: Kubernetes analyzer Jobs use the tool's native interface

In Job mode, Marketplace MUST support an unmodified SkillSpector CLI image without a Marketplace-specific entrypoint or service. Operators alone SHALL select analyzer images, arguments and report formats. Each analyzer MUST run outside the worker, non-root, with bounded private scratch space, read-only input, no credentials or repository mount, and denied ingress and egress.

#### Scenario: Stock CLI returns a private index-only skill report

- **WHEN** an isolated Job scans an index-only skill containing an inert dependency fixture with SkillSpector's offline static invocation
- **THEN** Marketplace MUST recover the native JSON's coverage and supported finding for that revision without exposing raw output or private skill bytes
- **AND** the CLI MUST have no service-account token, runtime socket, or access to other skills, and its outbound connection attempts MUST fail

#### Scenario: Registry content cannot configure execution

- **WHEN** the indexed registry supplies a purported analyzer image, scan target, extra arguments, or command
- **THEN** it MUST NOT change the Job, its permissions, or the operator-selected invocation

#### Scenario: Untrusted output or interrupted attempt is contained

- **WHEN** a Job times out or produces missing, malformed, oversized or non-regular output
- **THEN** the collector MUST NOT publish findings from that output or follow references outside the attempt
- **AND** temporary inputs and outputs MUST be reclaimed after success, failure or cancellation, including after dispatcher restart
