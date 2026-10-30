## Context

The plugin reference material already exists: the generated plugin support
reference (`site/docs/reference/plugin-support.md`, produced by
`make catalog-docs`) and plugin rows in the CLI, configuration, and lockfile
references. What is missing is task and concept documentation, and a
release note for teams whose members run different client versions.

## Goals / Non-Goals

**Goals:**

- One how-to and one explanation page, following the site's Diátaxis split.
- Every command, field, diagnostic, and harness claim matches shipped
  behavior and the archived plugin specifications.
- A curated release note that names the minimum client version.

**Non-Goals:**

- No behavior, configuration, lock, or diagnostic changes.
- No change to which harnesses are declared verified for native delivery.

## Decisions

- The how-to links to the reference pages for full field and diagnostic
  tables instead of repeating them.
- Native delivery is described as the generated reference states it; the
  pages never claim a harness is verified when its catalog says otherwise.
- The minimum version is stated as v5.2.0, the first release that reads
  plugin configuration. The release note is a `Breaking:` entry because
  older clients fail closed on a committed plugin source.

## Risks / Trade-offs

- [Docs drift from catalogs] → The support matrix stays generated, and the
  prose pages link to it instead of copying its table.
