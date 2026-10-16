# How to install resource bundles

Install a named [collection](../glossary.md#collection) of resources, or every resource in a registry, with one command.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- For a collection: a configured registry that publishes one.
- For a whole registry: its URL, and a repository with `.agents/config.yaml`.

## 1. List available collections

```bash
agent-marketplace list --kind collection
```

Expected output:

```text
REGISTRY  KIND        NAME                 TAGS (metadata)  STATUS
example   collection  platform-onboarding                   available
```

## 2. Install a collection

Install the collection in repository scope (the default):

```bash
agent-marketplace install platform-onboarding
```

The CLI adds `collection/platform-onboarding` to the source that publishes it in `.agents/config.yaml`, then syncs. It prints:

```text
Installed collection platform-onboarding in this repo
```

Check the selection:

```bash
agent-marketplace status
```

Under `SYNCED RESOURCES`, each member of the collection appears as its own row, and the `VIA` column shows `collection:platform-onboarding`.

## 3. Install all resources from a registry

To install everything in a registry folder that is not yet configured:

```bash
agent-marketplace install \
  --from https://github.com/example-org/agent-resources.git \
  --ref v1.4.0 \
  --path resources
```

The CLI adds the registry as a source in `.agents/config.yaml`, selects all of its resources with `all: true`, and syncs. It prints the bare installed names it selected, for example `Installed code-review, release in this repo`. Add `--scope user` or `--scope local` to write to a different configuration file. The scope label in the line changes to `user-wide` or `locally` to match.

If the last folder name in `--path` matches a resource name, the CLI installs only that resource.

## Troubleshooting

- If the CLI reports `resource or collection "platform-onboarding" not found in configured registries`, no configured registry publishes that collection. Check the registry's `path` and `ref`.
- To drop one member, run `agent-marketplace remove <member-name>`. It prints `Excluded <member-name> (provided by collection:platform-onboarding)`. The collection stays selected, and the CLI adds an exclusion for that member to the same configuration file.
