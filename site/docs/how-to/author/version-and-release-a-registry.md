# How to version and release a registry

This guide shows how to tag a registry release and update consumers safely.

## Prerequisites

- A Git registry on a platform such as GitHub.
- Push access to the repository.

## 1. Determine the version

Use semantic versioning. A new skill or a non-breaking improvement to an existing one is a minor bump. A structural or output change is a major bump.

## 2. Update a changelog

Record what changed in `CHANGELOG.md`:

```text
## 1.5.0

- Add `code-review` skill.
- Update `frontend-design` instructions for the new style guide.
```

## 3. Commit and tag

```bash
git add .
git commit -m "Release 1.5.0"
git tag v1.5.0
git push origin main v1.5.0
```

## 4. Update consumer references

A consumer pinned to the old tag must either run `agent-marketplace update --bump` when the ref is eligible for bumping, or edit the ref in `.agents/config.yaml` to `v1.5.0` and run `agent-marketplace sync`. Plain `agent-marketplace update` does not rewrite the old tag.

## 5. Notify consumers of breaking changes

If the release contains breaking changes, communicate the migration steps. Provide a `migrate` command or configuration snippet when needed.

## Troubleshooting

- **Consumers do not see the new tag**: The tag must be pushed to the origin the consumer fetches. Check the registry URL and ref.
- **`update --bump` does not rewrite the ref**: `--bump` rewrites only eligible refs to newer matching tags. `main` and other non-tag refs are not rewritten.
