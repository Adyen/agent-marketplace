# Local Test Registry

A local (non-git) registry for manually testing collections without needing a remote.

## Layout

```
testdata/local-registry/
├── agent-marketplace            -> symlink to skills/agent-marketplace
├── agent-marketplace-development -> symlink to skills/agent-marketplace-development
└── collections/
    └── agent-marketplace-tools/
        └── COLLECTION.yaml      ← the collection definition
```

## Using it

Point a personal-local config at this directory as a `directory` source (no `ref:` — directory sources skip git). In repository `.agents/` files, relative paths resolve from the repository root:

```yaml
# .agents/config.local.yaml  (gitignored, personal)
apiVersion: agent-marketplace/v4
sources:
  local-tools:
    directory: ./testdata/local-registry
resources:
  local-tools: [collection/agent-marketplace-tools]
```

Then:

```sh
agent-marketplace sync
agent-marketplace list --kind collection
agent-marketplace status
```
