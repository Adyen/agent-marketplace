# Git registries and hosted Marketplaces

`agent-marketplace` can acquire resources from plain Git registries and from a hosted Marketplace. They solve different problems and a consumer can use both at once.

## Git registries

A Git registry is any Git repository that contains resources in a supported harness layout. The consumer names a URL, ref, and optional path; the CLI resolves the ref to an exact commit, records it in a lockfile, and materializes from that commit.

Git registries need nothing but Git. Trust rests on the transport (SSH keys or HTTPS credentials you already have) and on the committed lock, which pins the commit and a digest of every resource so that a rewritten branch cannot change what a `sync` produces.

Identity in a Git registry is the tuple `(registry URL, ref, path, kind, name)`. The installed name is the resource's own name. When two registries select the same kind and name, the CLI picks a deterministic winner and warns; see [Scopes, selection, and precedence](./scopes-selection-and-precedence.md).

## Hosted Marketplaces

A hosted Marketplace is an HTTP service that an operator runs. It clones approved registries, ingests their resources, applies license and content policy, computes content digests, and serves a searchable catalog plus immutable artifacts. Consumers search it and install exact revisions.

A Marketplace adds what plain Git cannot:

- **Curation.** Only registries the operator approves appear, and the operator can revoke a registry, a resource, or one revision.
- **Policy.** License allowlists, size limits, and hidden-content rules are enforced at ingestion, before consumers ever see a resource.
- **Review before install.** Every install is prepared from a verified revision (digest, license, findings, capabilities, provenance). Security findings require consent; capability metadata remains visible but does not independently gate installation.
- **Availability.** In mirror mode the consumer never needs to reach the registry origin.

### Endpoint identity

A Marketplace endpoint is identified by its canonical URL and an `instance_id` published at `/.well-known/agent-marketplace.json`. `agent-marketplace install <url>` fetches that document, verifies both, and records them in configuration. Later commands re-verify the `instance_id`, so a URL that starts pointing at a different deployment is refused rather than trusted. An optional `alias` gives shorter locators.

### Resource identity, revisions, and installed names

A Marketplace resource has three separate identities:

| Identity | Example | Purpose |
|---|---|---|
| Coordinate | `team-marketplace/shared-agents/skill/review` | Which resource this is: `instance_id/registry/kind/name` |
| Revision | `sha256:0123…` | Which exact content: a SHA-256 digest of the canonical resource, immutable once published |
| Archive digest | `sha256:89ab…` | Which exact bytes the Marketplace served for that revision |

The consumer pins the coordinate and revision in `marketplaces[].resources`. Updating the resource in its registry produces a new revision; the old one stays retrievable and pinned consumers keep getting it until they run `update`.

Separately from all three, the **installed name** is the directory or file name the resource takes in your harness layout. It defaults to the resource name, but `install --as <name>` changes it, for example to avoid a collision with a Git-registry skill of the same name. The coordinate is unchanged; only the generated path differs.

### Locators

| Form | Shape |
|---|---|
| Compact | `<alias>:<registry>/<kind>/<name>` |
| Compact, exact | `<alias>:<registry>/<kind>/<name>@sha256:<64 hex>` |
| URL | `https://host/r/<registry>/<kind>/<name>` |
| URL, exact | `https://host/r/<registry>/<kind>/<name>/revisions/sha256:<64 hex>` |

`<kind>` uses the protocol spelling: `mcpserver`, not `mcp`. Details are in the [locators reference](../reference/marketplace-resource-locators.md).

### Mirror and index-only acquisition

The operator chooses, per registry or as an instance default, where the consumer gets the bytes:

| | `mirror` | `index-only` |
|---|---|---|
| Who serves the artifact | The Marketplace | The registry origin (the consumer fetches the exact commit) |
| What the Marketplace publishes | Metadata plus the archive | Metadata plus the exact commit and source-manifest digest |
| Consumer verification | Archive digest and content digest of the downloaded artifact | Content digest of the resource re-ingested from the exact commit, checked against the recorded manifest digest |
| Consumer pin fields | `digest` (+ `archive` digest in metadata) | `digest`, `registry_commit`, `registry_manifest_digest` |
| Good for | Speed, availability, audit of served bytes, no origin access needed | Registries that may not be mirrored for license or policy reasons; keeping a direct relationship with the origin; smaller Marketplace storage |

Both modes install through the same locators and produce the same harness output. Only the pin fields and the network path differ.

## Choosing between them

| Question | Git registry | Hosted Marketplace |
|---|---|---|
| Who can publish? | Anyone with a Git repository | Only registries the operator approves |
| What pins the content? | Commit in `.agents/lock.yaml` | Content digest in `.agents/config.yaml` |
| Where is policy enforced? | Nowhere; you review what you install | At ingestion, by the operator |
| What infrastructure is needed? | None beyond Git | An API, a worker, a database, a blob store, and a proxy |
| Can content be pulled after the fact? | No; a locked commit stays installable while the origin serves it | Yes; revocation blocks installs and fails `sync --check` |

Git registries appear under `registries` and use lockfiles; Marketplace pins appear under `marketplaces`. A repository can carry both.

## Related documentation

- [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md)
- [Marketplace resource locators reference](../reference/marketplace-resource-locators.md)
- [Marketplace architecture](./marketplace-architecture.md)
- [Marketplace trust model](./marketplace-trust-model.md)
