# How to approve and deploy a place target

Deploy a Marketplace place package to an application directory the CLI manages by ownership record rather than by harness compilation. A place target is an exact target profile, selected and locked by its full identity, that packages are assessed and placed against. Approval alone does not enable a target or run its checks.

This release ships no real host application profile. Profiles come from a Marketplace that advertises the `target-profile-place-v1` feature and serves `target-profile` records and the packages assessed against them.

## Prerequisites

- `agent-marketplace` on your `PATH`, on macOS or Linux.
- An existing v4 user configuration and a profile admitted by organization policy.
- The profile's canonical target directory must exist before approval.
- A registered Marketplace endpoint that serves the target profile and its packages. See [how to install from a hosted Marketplace](./install-from-a-marketplace.md) to register one.

## 1. Select the profile and package

Add the exact profile and package revisions to the user configuration. Merge these entries into your existing `sources` and `resources` maps, replacing the illustrative registry, target, package, and digest values:

```yaml
sources:
  hosted: {marketplace: "https://marketplace.example.com"}
resources:
  hosted:
    include:
      - tools/target-profile/example-app: {revision: "sha256:<profile-digest>"}
      - tools/example-app%2Fmod/sample: {revision: "sha256:<package-digest>"}
```

The package's kind is `example-app/mod`. Use `%2F` for the slash inside a scoped kind in a locator. Leave the place target disabled for now and run `agent-marketplace sync` to write the exact acquisition lock and cache the profile. This step does not download the place package archive. Direct `install` is not supported for scoped packages.

## 2. Approve the target

```bash
agent-marketplace target approve example-app
```

The command prints the full approval binding — OS, profile digest, canonical target root and state root, declared command roles, and the executable digest when the profile declares commands — and asks you to type `yes` in a terminal. `--yes` is refused. The recorded receipt names the future installation ID for that approved root; approval alone does not enable the target or install anything.

An organization can instead ship exact preapprovals in `target_preapprovals:` in the organization root. Preapproval matches only an exact canonical root and state root; a mismatch produces no receipt, and a changed root, alias, or `XDG_STATE_HOME` needs new approval — it never relocates an existing installation.

## 3. Enable the target and sync

```bash
agent-marketplace target enable example-app
agent-marketplace sync
```

`target enable` adds only the target ID to your user target list (`harnesses:` or `targets:`); it refuses targets that are not selected, unknown, or blocked by organization policy. `sync` then stages each package outside the target root, publishes files in dependency order, commits ownership, and runs the profile's approved initial-load check.

Headless `sync` skips a target that is still waiting for consent and continues other work; `sync --check` stays read-only and reports `target.place.out-of-sync`. Collections keep members for unusable targets and report each skip with its reason.

If a sync is interrupted, `status` and `sync --check` return `dependency.recovery-required` without replaying anything. Run a writing `agent-marketplace sync` to complete or stop at the journaled point; every stop names the journal and the path where it stopped.

## 4. Verify an installation

```bash
agent-marketplace target verify 'example-app%2Fmod/sample' --installation "<installation-id>"
```

`target verify` runs only the named approved check for an already-installed package and records the exact revisions, host-version status, and result. It installs, activates, and repairs nothing, and it refuses without running anything when the files have drifted. The installation ID is the one the approval binding reported; `status` also reports recorded verification results. Edited or deleted package files are reported as drift, keep the whole installed projection, and block upgrades as a unit.

## 5. Disable or retire

```bash
agent-marketplace target disable example-app
```

`target disable` removes only the target ID from the list. The profile selection, approval receipt, installed files, and ownership records all remain, and a later `target enable` restores deployment.

To remove an installation entirely, retire it by installation ID in a terminal:

```bash
agent-marketplace target retire "<installation-id>"
```

The command previews the recorded root, the files it will remove, and the files it will cede, then requires typing `yes`; `--yes` and non-terminal use are refused. Only unchanged owned files are removed — edited and unowned bytes stay byte-for-byte, and edited files are reported as ceded. Selection, receipts, locks, and caches are untouched. If a recorded parent directory cannot be proven, retirement refuses without changing the installation. Disable the target first if you do not want a later sync to reinstall a still-selected package.

## Related documentation

- [How to install from a hosted Marketplace](./install-from-a-marketplace.md)
- [How to troubleshoot sync drift](./troubleshoot-sync-drift.md)
- [Reference: Command line](../reference/cli.md#target)
- [Reference: Configuration layers](../reference/configuration-layers.md)
