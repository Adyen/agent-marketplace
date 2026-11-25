# Tasks

## 1. Regression Tests

- [ ] 1.1 Add isolated red command test `TestDoctorRepairDriftWithTransientOrganizationMarketplace` for confirmed drift repair followed by adoption, unchanged organization configuration and writable acquisition-lock bytes, absent organization acquisition lock, and continued refusal by standalone `sync --locked`; add `// spec: doctor-repair-planning/drift-repair-leaves-acquisition-locks-byte-identical` and `// spec: doctor-repair-planning/drift-repair-uses-verified-transient-organization-evidence` annotations, and verify the test reproduces the current failure with `go test ./cmd/agent-marketplace -run '^TestDoctorRepairDriftWithTransientOrganizationMarketplace$' -count=1`.
- [ ] 1.2 Add isolated guard test `TestDoctorRepairDriftRefusesUnverifiedOrganizationMarketplace` for both present-invalid and absent/unverifiable organization evidence, proving selection-dependent steps do not run and evidence remains unchanged; add `// spec: doctor-repair-planning/drift-repair-refuses-invalid-organization-evidence`. This test may already pass on the base.

## 2. Doctor Repair Route

- [ ] 2.1 Implement the doctor-only internal locked sync route that admits verified transient organization acquisition evidence from command context only when deployed evidence is absent, retaining lock leases, policy generation assertions, verification, and collision handling without calling unlocked sync; verify both regression tests pass with `go test ./cmd/agent-marketplace -run '^TestDoctorRepairDrift(WithTransientOrganizationMarketplace|RefusesUnverifiedOrganizationMarketplace)$' -count=1`.

## 3. Integration Validation

- [ ] 3.1 Run `make validate` and verify selected tests, CI lint, and build succeed.
- [ ] 3.2 Run `make spec-check` and verify strict OpenSpec validation and scenario trace succeed.
- [ ] 3.3 Run `make validate-e2e` and verify the Marketplace E2E and configuration-compatibility witnesses pass.

## Workflow follow-up

- Archive the change with `openspec archive doctor-drift-org-authority --yes` in the implementation delivery.
