## MODIFIED Requirements

### Requirement: Explicit modal intent

Toggle and config-editor accelerators SHALL retain their documented actions; sync-and-quit and quit modals SHALL retain affirmative, negative and cancel paths. The TUI SHALL NOT offer a consumer migration modal or an automatic conversion action. Unsupported consumer configuration SHALL surface the path-specific diagnostic defined by `consumer-config-format` and block configuration-dependent actions rather than enter legacy operation. q, Esc and Ctrl+C SHALL be routed before a form may submit. Modal submission SHALL use the existing serialized guarded operation and authoritative post-write reload; no form completion alone SHALL claim a write succeeded.

#### Scenario: Cancel or stale completion

- **WHEN** a user cancels a supported modal or a late write completion arrives for an inactive operation
- **THEN** no unintended write occurs and the active operation and diagnostics remain unchanged

#### Scenario: Guarded submission

- **WHEN** a user submits a mutation while another operation is active or prerequisites are invalid, including unsupported consumer configuration
- **THEN** the same guard used by the keyboard path blocks the new write without claiming success or offering conversion
