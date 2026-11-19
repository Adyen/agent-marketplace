# Operator preconditions

- [ ] 0.1 Confirm against the real Claude Enterprise organization whether an
  administrator can create a second organization-owned `manual` marketplace in
  claude.ai, or whether the library marketplace is the only upload target. Record the
  answer in `design.md` D3. Verify by listing
  `GET /v1/organizations/plugin_marketplaces?owner_type=organization` with a
  `read:plugins` key and comparing against what the claude.ai UI offers
- [ ] 0.2 Confirm the Admin API key, its `read:plugins` and `write:plugins` scopes,
  and the `anthropic-version: 2026-10-21` plus
  `anthropic-beta: ce-plugins-2026-09-01` headers against a live read request.
  Verify `GET /v1/organizations/plugins?limit=1` returns `200`
- [ ] 0.3 Measure the organization's current plugin count and resource count of the
  registries to be mirrored, against the 1,000-plugin marketplace limit (design D2).
  Verify the first full mirror fits, and record the headroom in the change
