## MODIFIED Requirements

### Requirement: Repair presents a numbered plan and one confirmation

Before mutation, repair SHALL number the steps and identify class, action, affected outputs and blocker in both text views. Detailed text SHALL also identify codes, paths and inputs. On a terminal one `[y/N]` confirms the presented safe steps only on explicit `y` or `yes`. Destructive entries SHALL require per-entry consent and no plan confirmation if none are safe. Neither view SHALL reveal MCP values.

#### Scenario: Repair plan lists every step before confirmation

- **WHEN** `doctor --repair` runs on a terminal with at least one safe step to run
- **THEN** it SHALL identify every step, class, affected output and applicable blocker before consent, with codes, full file paths where known and required inputs available in the detailed plan
- **AND** it SHALL ask exactly one plan-level `[y/N]` confirmation before running any step

#### Scenario: Declining the repair plan changes nothing

- **WHEN** the answer to the plan-level confirmation is anything other than `y` or `yes`, including an empty line or end of input
- **THEN** repair SHALL run no step and leave every configuration, lock, ownership-state, and harness file byte-identical
- **AND** repair SHALL exit 0 even when corrupt state or blocked, guided, or human-only items remain, unless missing or stale acquisition authority remains, in which case it SHALL exit 1

## ADDED Requirements

### Requirement: Human repair output separates decisions from inspection detail

Human repair SHALL lead with decisions, not healthy inventory. Both views SHALL show failures and remedies, safety warnings, destructive or blocked entries, guided and human-only actions, required confirmations and backup/digest details. Hidden advisory detail SHALL be signposted with `doctor --repair --verbose`. The detail switch SHALL NOT affect diagnosis, consent, execution, exit status, plain `doctor` output or the single-document JSON interface.

#### Scenario: Mixed safe repairs lead with the decision

- **WHEN** a human previews drifted skill caches and exact shared-entry adoptions alongside healthy harness targets and repeated advisory compiler warnings
- **THEN** the default view SHALL first identify the safe changes, the affected skills and shared entries and the byte-neutral effect of adoption, without a healthy install-root dump, empty inspections or repeated advisory warnings
- **AND** both views SHALL name each offered adoption or replacement and its source resource, and the default view SHALL omit nested sync install totals
- **AND** preview without consent SHALL NOT apply any step

#### Scenario: Detailed repair retains the inspection trail

- **WHEN** a setup with both actionable and healthy shared files is previewed with `doctor --repair --verbose`
- **THEN** the detailed view SHALL show healthy roots, inspected shared-file paths and counts, diagnostic details and the same actionable steps and consent requirements
- **AND** a blocker or destructive conflict SHALL remain visible even without verbose output, with actionable guidance and no MCP values

### Requirement: Human repair reports the result of the whole plan

Human repair SHALL report outcomes by affected output and declare clean once only after a complete, empty diagnosis. Compact re-diagnosis SHALL show changed or new work before applicable consent without replaying unchanged steps. A successful subcommand alone SHALL NOT establish that drift cleared. Failed, recurring, blocked, deferred, declined or preview-only work SHALL NOT be reported as repaired.

#### Scenario: Cache repair is not an install count

- **WHEN** authorized cache-backed skill drift is reconciled without replacing its installed link and subsequent diagnosis finds no remaining steps
- **THEN** human output SHALL identify the reconciled skill and end with one clean result after re-diagnosis, without using `Installed 0` as the compact repair result

#### Scenario: Empty ownership is not an empty plan

- **WHEN** ownership inspection has no candidates but the plan contains drift, a blocker or deferred work
- **THEN** human output SHALL identify the remaining work and SHALL NOT announce that there is nothing to repair
- **AND** if the drift step keeps recurring to the iteration cap, the final failure SHALL identify the remaining output without claiming success
