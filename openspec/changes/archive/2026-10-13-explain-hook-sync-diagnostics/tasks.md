## 1. Tests

- [x] 1.1 Add pipeline regression tests for hook error target retention, target dedup, and sanitization
- [x] 1.2 Add hookpolicy refusal-stage tests with controls
- [x] 1.3 Add a CLI consumer test for mixed Devin matcher refusals and check read-only behavior
- [x] 1.4 Add hook review action label test and update drift summary wording tests

## 2. Implementation

- [x] 2.1 Render and dedup target-qualified hook error diagnostics in the pipeline
- [x] 2.2 Split matcher refusal messages and hints by translation stage
- [x] 2.3 Present hook errors through the promoted diagnostic in sync --check and label hook review actions
- [x] 2.4 Update drift summary wording to writes, removals, and outputs
