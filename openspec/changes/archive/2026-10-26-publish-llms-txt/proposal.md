## Why

LLM agents and tools that read `llms.txt` (llmstxt.org) have no index of the documentation and no raw Markdown source to fetch. The HTML-only site makes them scrape navigation chrome to find pages.

## What Changes

- Each documentation deployment profile publishes `llms.txt` at its profile root, listing every documentation page in navigation order.
- Each listed page is also published as a raw Markdown copy beside its HTML route, without front matter. Both profiles emit the same Markdown copies and index.
- The sidebar in `site/.vitepress/config.mts` becomes the single source for the index and the copies. The build fails if a documentation page has no sidebar entry.
- `site-deployment-profiles` adds "Documentation publishes an LLM-readable index".

## Capabilities

### New Capabilities

### Modified Capabilities
- `site-deployment-profiles`: documentation publishes an LLM-readable index and Markdown copies in both profiles.

## Impact

`site/.vitepress` build (new `llms.ts`, `buildEnd` hook), `site/Dockerfile` nginx `text/markdown` content type, `site/tests`, and `site/README.md`. No new npm dependency.

## Release note

The documentation site now publishes `llms.txt` and a Markdown copy of every documentation page, so LLM tools can read the docs without parsing HTML.
