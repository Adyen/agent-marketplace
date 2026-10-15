# How to remove agent-marketplace safely

Remove the files `agent-marketplace` generated in a repository or on your machine without deleting your own files. The `deinit` command does the removal. You choose what it covers with `--scope`:

| Scope | What `deinit` removes | What `--purge-config` also deletes |
|---|---|---|
| `user` (the default) | Machine-wide generated output, plus cached registry clones that your organization's configuration does not use | `~/.agents/config.yaml` and `~/.agents/lock.yaml` |
| `repo` | Generated output in the current repository | `.agents/config.yaml` and `.agents/lock.yaml` |
| `local` | The same repository output as `repo` | `.agents/config.local.yaml` and `.agents/lock.local.yaml` |

## Prerequisites

- `agent-marketplace` on your `PATH`.
- For repository removal, a terminal open inside the repository.
- Optional: run `agent-marketplace backup create` first if you want an archive of the machine-wide harness folders before anything is removed. `deinit` does not create one for you. See [How to back up and restore machine resources](./back-up-and-restore-machine-resources.md).

## Remove from a repository

1. Preview what `deinit` would remove:

   ```bash
   agent-marketplace deinit --scope repo --dry-run
   ```

   The preview lists the generated files and folders and ends with a line such as `Would remove 4 managed artifact(s)`. Nothing changes on disk.

2. Run the removal:

   ```bash
   agent-marketplace deinit --scope repo
   ```

   The run prints `Removed N managed artifact(s)`, or `No managed artifacts to remove` when there is nothing to remove. Unlike the preview, it does not list the individual files. This deletes the generated [harness](../glossary.md#harness) output in the repository and nothing else. Your configuration files, lockfiles, organization policy, and the durable ownership state on your machine stay in place, so a later `agent-marketplace sync` can put everything back.

3. Delete the repository configuration too, but only if you no longer want it:

   ```bash
   agent-marketplace deinit --scope repo --purge-config
   ```

   After the artifact summary, it prints `Removed config <path>` and `Removed lock <path>`, one line for each file that actually existed, so an absent file produces no line. It removes the `.agents` folder if it is left empty. It does not touch `.agents/config.local.yaml` or `.agents/lock.local.yaml`; delete those with `--scope local --purge-config`. Files that your configuration pulls in with `include` are not deleted.

4. Run `git status`, which lists the deleted files under `Changes not staged for commit`. Review them and commit the changes you want to keep.

## Remove from a machine

1. Preview the machine-wide removal:

   ```bash
   agent-marketplace deinit --scope user --dry-run
   ```

   The preview lists the generated output and each cache folder it would delete (`Would remove cache ...`).

2. Run the removal:

   ```bash
   agent-marketplace deinit --scope user
   ```

   The run prints `Removed N managed artifact(s)` (or `No managed artifacts to remove`) and then one `Removed cache <path>` line for each cache folder it deletes. This removes the machine-wide generated output and the cached registry clones under `~/.cache/agent-marketplace/` that your organization's configuration does not use. That includes clones your repositories still use; the next `sync` in each repository downloads them again. Resources that your organization's configuration still selects stay installed, together with the cache they need; if one of them is missing, `deinit` writes it again.

3. Delete your personal configuration, but only if you no longer want it:

   ```bash
   agent-marketplace deinit --scope user --purge-config
   ```

   After the artifact and cache output, it prints `Removed config <path>` and `Removed lock <path>` for each file that actually existed. This also deletes `~/.agents/config.yaml` and `~/.agents/lock.yaml`.

## What `deinit` leaves alone

- The durable state folder `${XDG_STATE_HOME:-$HOME/.local/state}/agent-marketplace/`, which holds the [ownership state](../glossary.md#ownership-state) and safety backups for shared configuration files.
- Backup archives under `~/.agent-marketplace/backups/`.
- Files you wrote yourself inside harness folders, and handwritten entries, comments, and unrelated settings in shared configuration files such as `.mcp.json`.
- The organization configuration and lock under `/etc/agent-marketplace/`.
- Internal transaction files that `--purge-config` uses to coordinate its deletions.

## Troubleshooting

- `deinit` prints `warning: could not read config`: your configuration could not be loaded, so `deinit` assumed the default installation mode. Output that was installed in the other mode (symlinks versus regular files) may remain. Fix the configuration and run `deinit` again.
- `error: could not resolve organization-owned artifacts; refusing teardown`: a user-scope `deinit` must work out which resources the organization still requires before it removes anything, and it stops without changes when it cannot. If the organization lock is invalid, the organization administrator has to repair or regenerate it. Do not remove or edit organization policy yourself.
- Generated folders remain after `deinit`: run `agent-marketplace status` to see which scope still selects them. Repository output needs `--scope repo`, and machine output needs `--scope user`.
