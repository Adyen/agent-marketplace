# How to rename or retire a resource

Rename or delete a resource in your registry without breaking the people who use it. Consumers identify a resource by its kind and name, so to them a rename is a removal plus a new resource. [How upstream changes reach consumers](../../explanation/how-upstream-changes-reach-consumers.md) explains what each kind of consumer sees.

## Prerequisites

- Push access to a Git registry.
- A list of who consumes the registry, including any organization that selects it with `required: true` and any hosted Marketplace that indexes it.

## 1. Know who breaks

Before you change a name, check how the change lands for each consumer:

| Consumer selects the resource | What happens when the old name disappears |
|---|---|
| By name | The resource vanishes from their harness. They get a `selected but not found` warning and must install the new name themselves. |
| With `all: true` or `tags` | The new name installs in place of the old one. Their `exclude` of the old name stops working. |
| Through your collection | Nothing breaks if the collection lists the new name in the same commit. |
| As a collection that you rename | Every `sync` fails, unless you keep a collection with the old name (step 4). |
| With `required: true` | Every `sync` fails until they change their configuration. |
| From a hosted Marketplace | Their pinned revision keeps working, but `agent-marketplace update` fails with `resource not found` until they replace the pin. |

Required and Marketplace consumers break hardest, so give them the most notice.

## 2. Publish the new name next to the old one

Add the resource under its new name and keep the old one for at least one release. Mark the old one as deprecated in its description, so it shows in listings and in the harness:

```bash
cp -R skills/code-review skills/review
```

Edit `skills/review/SKILL.md` so its frontmatter `name` matches the new folder:

```yaml
---
name: review
description: Review a change for correctness, style, and test coverage.
---
```

Edit `skills/code-review/SKILL.md` to point users at the new name:

```yaml
---
name: code-review
description: "Deprecated: renamed to review. Install review instead; code-review will be removed in 3.0.0."
---
```

During this window, consumers who select everything get both copies. That is the price of not breaking consumers who select by name.

## 3. Update collections in the same commit

Change every collection member that names the old resource to the new name:

```yaml
# collections/team/COLLECTION.yaml
name: team
resources:
  - kind: skill
    name: review
```

Collection consumers then switch to the new name on their next update and never see the deprecated copy. A collection that still lists a deleted name produces a warning on every consumer's sync, and fails `sync --strict`.

## 4. Keep the old name when you rename a collection

Consumers select a collection by its `name` field. If that name disappears, every consumer's `sync` fails, even without `required: true`:

```text
Error: dependency-committed sync failure: collection "team" was selected but not found in https://github.com/example-org/agent-resources.git@main
```

When you rename a collection, keep a collection with the old name that includes the new one and lists no resources of its own:

```yaml
# collections/team/COLLECTION.yaml
name: team
description: "Deprecated: renamed to crew."
includes:
  - name: crew
```

Consumers who select `collection/team` keep the same members with no warning, and `status` shows them as `VIA collection:team > collection:crew`. Delete the old collection in a later major release, as in step 6.

## 5. Release and announce

Release the overlap as a minor version, following [how to version and release a registry](./version-and-release-a-registry.md). In the changelog, give consumers the exact steps:

```text
## 2.4.0

- Rename `code-review` to `review`. `code-review` remains as a deprecated copy until 3.0.0.
  - If you select it by name: `agent-marketplace install review`, then remove `skill/code-review` from your configuration.
  - If you exclude `skill/code-review`: also exclude `skill/review`.
  - If you pin it from a Marketplace: `agent-marketplace remove code-review`, then install `review` from the same Marketplace.
  - If you select it with `required: true`: change the entry to `skill/review`.
```

Tell administrators of organization configurations directly. A required selection of a deleted name fails on every managed machine as soon as they upgrade their lock.

## 6. Delete the old name

After the overlap release has been out long enough, delete the deprecated folder:

```bash
git rm -r skills/code-review
git commit -m "Remove deprecated code-review skill"
```

Release this as a major version. Consumers who still select the old name lose it on their next update and get the `selected but not found` warning, which tells them to run `agent-marketplace status` or remove the selection.

## Retire a resource without a replacement

Follow the same steps without the new copy: mark the description as deprecated, drop it from collections, announce the removal, and delete it in a major release.

## Verify

Check that both names are present during the overlap, from a consumer checkout that uses your registry:

```bash
agent-marketplace update
agent-marketplace list
```

`list` shows both `code-review` and `review`. Then confirm that no collection member points at a missing name:

```bash
agent-marketplace sync --strict
```

It exits `0` when every selected and collected name exists. Add this check to your registry CI; see [how to validate a registry in CI](./validate-a-registry-in-ci.md).

## Related documentation

- [How upstream changes reach consumers](../../explanation/how-upstream-changes-reach-consumers.md)
- [How to handle a renamed or removed resource](../handle-renamed-or-removed-resources.md)
- [Collections reference](../../reference/collections.md)
