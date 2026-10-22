## 1. Delivery resolution and registration

- [x] 1.1 Resolve `delivery` per harness target from its catalog and report
  the choice; refuse `native` to an unverified target with
  `plugin.delivery.native-unverified` (design D1); annotate the delivery
  choice scenarios
- [x] 1.2 Add the Claude Code and Codex registration members to
  `NativeLayout`, the native plugin catalog under the state root, and the
  combined owned-entry mutation with hooks and MCP; fail closed for targets
  without registration members (design D2). Annotate the ownership and
  preservation scenarios with `delivery: native` forced by a test-only
  verified catalog (design D5)

## 2. Transitions and reload reporting

- [x] 2.1 Report `reload-required` from the catalog refresh field (design
  D3); annotate its scenario
- [x] 2.2 Implement delivery transitions (design D4); annotate the
  transition scenarios, including an `AGENT_MARKETPLACE_E2E_CRASH_AT`
  interruption test with no staging residue

## 3. Gates

- [ ] 3.1 Run `make validate`, `make spec-check`, and `make validate-e2e`
