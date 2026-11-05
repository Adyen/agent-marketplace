## Why

HTTP(S) acquisition reads trusted Git configuration only from files owned by root or the current user. Package managers can install Git under a dedicated account. Homebrew's macOS installer honours an administrator-defined install user, and multi-user Linuxbrew uses a `linuxbrew` account. Git's compiled-in system configuration and the `git` executable then belong to that account. Whenever that file declares any credential, HTTP or include setting, every HTTPS registry fails with `git.auth-config-untrusted`, although the same `git clone` works (#632).

## What Changes

- Also trust configuration files owned by the account that owns the `git` executable acquisition runs, found by the `PATH` lookup and resolved through symlinks. That account can already replace the program being executed, so trusting its files grants no new authority.
- Extend that trust only when the executable is a regular file not writable by group or others. All other file checks are unchanged.

## Capabilities

### Modified Capabilities

- `git-authentication`: trusted configuration owners include the owner of the executed `git`.

## Impact

No configuration surface or username allowlist. Root/current-user behaviour and every refusal control are unchanged. Installations whose Git belongs to a dedicated account can use HTTPS registries without disabling the system configuration.
