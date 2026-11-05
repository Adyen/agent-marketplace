# How to respond to a revoked resource

Get `sync` working again after a Marketplace operator revokes a revision that you have pinned. A revocation means the operator no longer wants anyone to install that revision. The operator side is covered in [how to revoke a resource revision](./operator/revoke-a-resource.md).

## Prerequisites

- A Marketplace pin in your user, repository, or personal-local configuration. For a pin in the organization file, see [step 4](#_4-handle-an-organization-pin).

## 1. Recognize a revoked pin

A revoked pin fails every `sync`:

```text
Installed 0  Removed 0  Malformed 0  Errors 1

Error: error: marketplace https://marketplace.example.com pin shared-agents/skill/review: revision is revoked
```

`sync` exits `1` and leaves the installed files in place. `sync --check` also exits `1` and lists the revoked resource's files as removals. `agent-marketplace status` shows `"revoked"` in the `STATE` column of `MARKETPLACE PINS`.

## 2. Move to a revision that is not revoked

If the operator revoked one exact revision and the registry has published a newer one, move the pin forward:

```bash
agent-marketplace update
```

Expected output:

```text
Configured registry entries: 0
Marketplace pins updated: 1
Marketplace "https://marketplace.example.com" "shared-agents"/"skill"/"review": updated "sha256:b316..." -> "sha256:1484..."
```

Then confirm that the pin is healthy:

```bash
agent-marketplace sync --check
```

It prints nothing and exits `0` when everything is in sync. Commit the lock if the pin lives in the repository configuration.

If the newest revision is the revoked one, `update` fails and changes nothing:

```text
Error: marketplace https://marketplace.example.com pin shared-agents/skill/review: latest revision: revision is revoked
```

Wait for the registry to publish a fixed revision, or go to step 3.

## 3. Remove the resource if no safe revision exists

An operator can also revoke every revision of a resource, or a whole registry. Those revocations cover revisions published later too, so `update` keeps failing with the same error. Remove the pin from the file that declares it:

```bash
agent-marketplace remove review --scope repo
```

It prints `Removed review`, removes the pin from the configuration and lock, and deletes the installed files. Install a replacement if one exists.

## 4. Handle an organization pin

Consumer `update` does not check or change pins in the organization file. If the organization group does not set `required: true`, you can hide the pin on your machine with an exclusion-only Marketplace source in `~/.agents/config.yaml`; see [hosted Marketplaces](../reference/configuration.md#hosted-marketplaces) for the syntax. Otherwise, ask the administrator to change the pin.

## Run one sync against a revoked pin

When you must sync before you can move off the revision, allow it for one run. Pass the full coordinate shown in `status`, in the form `<instance_id>/<registry>/<kind>/<name>@sha256:<digest>`:

```bash
agent-marketplace sync --allow-revoked team-marketplace/shared-agents/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Expected output:

```text
Installed 0  Removed 0  Malformed 0  Errors 0
```

The override applies to that run only and does not change your configuration or lock. The next plain `sync` fails again. You cannot combine it with `--check`, so CI checks stay red until you move off the revision.

## Related documentation

- [Marketplace trust model](../explanation/marketplace-trust-model.md)
- [How to install from a hosted Marketplace](./install-from-a-marketplace.md)
- [`sync` reference](../reference/cli.md#sync)
