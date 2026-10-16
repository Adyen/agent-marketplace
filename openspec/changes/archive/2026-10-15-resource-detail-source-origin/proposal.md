## Why

The resource detail page does not say where a resource came from. Users cannot tell which repository or path an installed resource originates from, and the header spends its metadata row on the scan date, which the Security tab already shows.

## What Changes

- Resource revision responses (latest, revision list, and exact revision) include an optional `source` object with the sanitized repository URL, the resource path, and the occurrence commit, reusing the catalog `CatalogSource` shape.
- The resource detail header shows the source repository and path as its first metadata item, linked to the resource at the occurrence commit on GitHub, GitLab, and bitbucket.org, or to the repository on other hosts.
- The header metadata row no longer shows the Scanned group; the scan date remains in the Security tab and the Revisions tab.
