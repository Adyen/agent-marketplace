# Git registries and hosted Marketplaces

`agent-marketplace` can get resources from two kinds of place: a plain Git [registry](../glossary.md#registry) and a hosted [Marketplace](../glossary.md#marketplace). They solve different problems, and one configuration can use both.

## Git registries

A Git registry is any Git repository that holds resources in a supported harness layout. You give the CLI a URL, a [ref](../glossary.md#ref) (a branch, tag, or commit), and optionally a path inside the repository. The CLI resolves the ref to one exact [commit](../glossary.md#commit), records that commit in a [lockfile](../glossary.md#lockfile), and writes the resources from that commit into your harness folders.

A Git registry needs nothing except Git. Access works through the SSH keys or HTTPS credentials you already use. The lockfile records the commit and a digest of every resource, so if someone rewrites the branch, a later `sync` still produces the same files while the recorded commit remains in the cache or fetchable from the server.

A resource in a Git registry is identified by the registry URL, the ref, the path, its kind, and its name. It installs under its own name. When two registries offer the same kind and name, the CLI picks one winner by fixed rules and prints a warning; see [Scopes, selection, and precedence](./scopes-selection-and-precedence.md).

## Hosted Marketplaces

A hosted Marketplace is a web service that an operator runs for a team or company. It clones the registries the operator approved, reads their resources, applies license and content policy, computes a content digest for each resource, and serves a searchable catalog with downloadable artifacts. You search it and install exact revisions.

A Marketplace adds things plain Git cannot give you:

- Curation: only approved registries appear, and the operator can revoke a whole registry, one resource, or one revision.
- Policy: license allowlists, size limits, and hidden-content rules run when the Marketplace reads a registry, before anyone can install from it.
- Review before install: the CLI shows the verified revision (digest, license, security findings, capabilities, and where it came from) before it installs. Security findings need your consent. Capabilities are shown but do not require consent on their own.
- Availability: in `mirror` mode you never need access to the registry itself.

The [Marketplace trust model](./marketplace-trust-model.md) explains these checks in more detail.

### Endpoint identity

A Marketplace endpoint is identified by its canonical URL and by an `instance_id` published at `/.well-known/agent-marketplace.json`. `agent-marketplace install <url>` reads that document, checks both values, and records them. Later commands check the `instance_id` again, so if the URL starts pointing at a different deployment, the CLI refuses it. An alias gives the endpoint a short name for locators.

### Resource identity, revisions, and installed names

A Marketplace resource has three separate identifiers:

| Identity | Example | Purpose |
|---|---|---|
| Coordinate | `team-marketplace/shared-agents/skill/review` | Which resource this is: `instance_id/registry/kind/name`. |
| Revision | `sha256:0123…` | Which exact content: a SHA-256 digest of the resource. It never changes after the Marketplace publishes it. |
| Archive digest | `sha256:89ab…` | Which exact bytes the Marketplace serves for that revision. |

The CLI records the coordinate and the exact revision when you install. When the resource changes in its registry, the Marketplace publishes a new revision. The old one stays available, and you keep getting it until you run `update`.

The installed name is a fourth, separate thing: the folder or file name the resource gets in your harness layout. It defaults to the resource name. `install --as <name>` (or the `as` option in a configuration file) changes it, for example to avoid a clash with a Git-registry skill of the same name. The coordinate stays the same; only the generated path changes.

### Locators

A locator names one Marketplace resource in compact form (`<alias>:<registry>/<kind>/<name>`) or as a URL on the endpoint, each with an exact variant that appends the revision digest. `<kind>` uses the Marketplace spelling: `mcpserver`, not `mcp`. The [locators reference](../reference/marketplace-resource-locators.md) has the full rules.

### Mirror and index-only acquisition

The operator decides, for each registry or as a default for the instance, where you download the content from:

| | `mirror` | `index-only` |
|---|---|---|
| Who serves the content | The Marketplace | The registry's own Git server (the CLI fetches the exact commit) |
| What the Marketplace publishes | Metadata and the archive | Metadata, the exact commit, and a digest of the registry's source manifest |
| What the CLI checks | The archive digest and content digest of the downloaded artifact | The content digest of the resource read from the exact commit, against the recorded manifest digest |
| What the lockfile records | Revision, content digest, and archive digest | The same, plus the registry commit, the source-manifest digest, and the resource's path |
| Good for | Speed, availability, an audit trail of the bytes served, no access to the registry needed | Registries that may not be mirrored for license or policy reasons, keeping a direct link to the registry, smaller Marketplace storage |

Both modes use the same locators and produce the same files in your harness. Only the recorded fields and the download path differ.

A Marketplace can repair a mirrored archive without changing the resource: the content digest stays pinned, but the served archive digest moves. A writing `sync` or `update` refreshes the lockfile's `archive_digest` to match, reports `marketplace.archive-evidence-refreshed`, and re-downloads the artifact; `sync --locked` refuses the drifted evidence and tells you to run one of those commands. The previously served archive remains downloadable by its old digest.

## Choosing between them

| Question | Git registry | Hosted Marketplace |
|---|---|---|
| Who can publish? | Anyone with a Git repository | Only registries the operator approves |
| What pins the content? | A commit in the lockfile (for example `.agents/lock.yaml`) | A content digest in the lockfile |
| Where is policy enforced? | Nowhere; you review what you install | When the Marketplace reads the registry, by the operator's rules |
| What infrastructure is needed? | Git only | An API, a worker, a database, a blob store, and a reverse proxy |
| Can content be withdrawn later? | No; a locked commit stays installable while the Git server still has it | Yes; revocation blocks installs and makes `sync --check` fail |

In a v4 configuration, both are named entries under `sources:`: a Git registry uses a `git:` URL and a Marketplace uses a `marketplace:` URL. The resources you pick from each go under `resources:`, and each configuration layer's lockfile records the exact commits and revisions. Older v2/v3 consumer roots are unsupported; [convert them with an older released migrator before upgrading](../how-to/migrate-configuration.md) rather than using legacy `registries` or `marketplaces` installation keys. See [Sources and resources](./sources-and-resources.md) for the v4 format.

## Related documentation

- [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md)
- [Marketplace resource locators reference](../reference/marketplace-resource-locators.md)
- [Marketplace architecture](./marketplace-architecture.md)
- [Marketplace trust model](./marketplace-trust-model.md)
