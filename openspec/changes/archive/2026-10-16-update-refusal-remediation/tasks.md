## 1. Tests first

- [x] 1.1 Add command tests in `cmd/agent-marketplace` covering every scenario of the `resource-selection` delta, each bound with a `// spec: resource-selection/<scenario-slug>` line, and observe the new remediation assertions fail on the base

## 2. Refusal remediation

- [x] 2.1 Print one remediation line after each named miss, using the non-required `remove --kind` text or the required-selection text
- [x] 2.2 Collect prior-revision snapshot load errors in the selection guard and print them as a note on refusal
- [x] 2.3 Replace the closing hint with the update-left-unchanged line that names `update --registry <url>`

## 3. Verification

- [x] 3.1 Focused update tests, `make spec-check`, and `make ci-lint` pass
