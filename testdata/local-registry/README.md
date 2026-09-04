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

Point a local config at this directory (no `ref:` — local registries skip git):

```yaml
# .agents/config.local.yaml  (gitignored, personal)
apiVersion: agent-marketplace/v2
registries:
  - url: ../testdata/local-registry
collections:
  include: [agent-marketplace-tools]
```

Then:

```sh
agent-marketplace sync
agent-marketplace list --kind collection
agent-marketplace status
```
