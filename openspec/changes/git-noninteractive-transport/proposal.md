## Why

A first-time user reported two failures in the TUI install-from-source flow:

1. A mistyped public HTTPS URL (`https://github.com/anthropic/skills.git` instead of `anthropics/skills`) failed with `git.auth-transport-failed: transport diagnostics withheld to protect authentication data`. GitHub answers a missing repository with HTTP 401, and the withheld diagnostic gives no hint that the URL may be wrong.
2. A web URL was first probed over SSH. On a machine without `github.com` in `known_hosts`, `ssh` opened `/dev/tty` and printed its host-key confirmation over the TUI while competing with it for key presses. `GIT_TERMINAL_PROMPT=0` already prevents HTTPS prompts, but nothing prevents `ssh` from prompting.

## What Changes

- Every Git subprocess runs without access to the controlling terminal, in the CLI and the TUI. `ssh` cannot ask for a host-key confirmation or a key passphrase; `SSH_ASKPASS` is never invoked. Unknown host keys are never accepted automatically.
- An SSH failure caused by an unverified host key reports `git.auth-transport-failed` with the host and how to verify and add its key to `known_hosts`. A web URL whose SSH probe fails this way still falls back to HTTPS as before.
- An HTTP(S) failure where Git reports failed authentication or a missing repository reports `git.auth-transport-failed: repository not found or requires credentials`, telling the user to check the URL or configure a credential helper. Git's own output is still never shown.

## Capabilities

### New Capabilities

### Modified Capabilities
- `git-authentication`: Git transport never prompts on the terminal, and transport failures name the likely cause.

## Impact

`internal/processstart` (terminal detachment on Unix), `internal/cache` (`registry.go` environment and diagnostics, `git_auth.go` classification), `internal/remedy` hint text, the private-registry guide, and a TUI QA journey for the install-from-source flow. Windows process creation is unchanged.

## Release note

Git operations no longer prompt on the terminal, so an unknown SSH host key can no longer corrupt the TUI; the error explains how to trust the host. A mistyped or private HTTPS repository now reports "repository not found or requires credentials" instead of only withholding the diagnostic.
