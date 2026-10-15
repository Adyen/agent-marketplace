# How to version and release a registry

Tag a registry release and tell consumers how to move to it.

## Prerequisites

- A Git registry hosted on a platform such as GitHub.
- Permission to push to the repository.

## 1. Choose the version

Use semantic versioning. A new skill or a compatible improvement to an existing one is a minor release. A change that renames or removes resources, or changes their output in a way consumers must adapt to, is a major release.

## 2. Update the changelog

Record what changed in `CHANGELOG.md`:

```text
## 1.5.0

- Add `code-review` skill.
- Update `frontend-design` instructions for the new style guide.
```

## 3. Commit, tag, and push

```bash
git add .
git commit -m "Release 1.5.0"
git tag v1.5.0
git push origin main v1.5.0
```

Consumers can only use a tag after you push it.

## 4. Move consumers to the new tag

A consumer whose source has `ref: v1.4.0` stays on that tag until the ref changes. Plain `agent-marketplace update` does not change it. The consumer has two options:

- Edit `ref:` in their configuration file to `v1.5.0` and run `agent-marketplace sync`. Sync updates the [lockfile](../../glossary.md#lockfile) to match.
- Run `agent-marketplace update --bump` to rewrite eligible refs in v4 `sources:` before updating locks and syncing. It rewrites a ref only when the ref ends in a number, and only to a tag that differs from it in that last number alone. `v1.4.0` can move to `v1.4.1` or `v1.4.7`, but never to `v1.5.0`. A ref such as `v1` can move to `v2`.

So `--bump` reaches patch releases of a full version tag. For a minor or major release, consumers edit the ref.

`--bump` edits only refs owned by the selected writable root. A ref declared by an included fragment is reported and left unchanged; edit that fragment deliberately if you want to advance it. Organization-managed refs are not rewritten by consumer `update`.

## 5. Announce breaking changes

For a release with breaking changes, tell consumers what to change: which resources were renamed or removed, and the configuration snippet they need.

## Troubleshooting

- Consumers do not see the new tag: check that the tag was pushed to the repository URL the consumer's source uses.
- `update --bump` leaves the ref unchanged: the ref is declared by an included fragment, does not end in a number (for example `main`), has no tag with the same prefix and a higher final number, or belongs to a local `directory` source. For a Git source, edit the owning root or fragment deliberately if you need a different ref.
