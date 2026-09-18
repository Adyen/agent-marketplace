# How to configure private Git registries

Authenticate `agent-marketplace` to a private Git registry without storing credentials in the `agent-marketplace` configuration or lock.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A private registry you can clone over HTTPS or SSH.
- Normal Git credentials already configured on the machine.

## Prefer SSH key authentication

The most portable option is to use an SSH URL and the standard SSH agent.

1. Ensure the key is available:

```bash
ssh-add ~/.ssh/id_agent_marketplace
```

2. Verify that Git can read the repository without mutating any configuration:

```bash
git ls-remote git@github.com:example-org/private-skills.git
```

3. Configure the registry in `agent-marketplace` with the same SSH URL:

```yaml
registries:
  - url: git@github.com:example-org/private-skills.git
    ref: main
```

## Use an HTTPS URL with existing Git authentication

If you normally authenticate to the host with a credential manager or personal access token, verify that `git ls-remote` works with your existing setup:

```bash
git ls-remote https://github.com/example-org/private-skills.git
```

Then configure the same URL in `agent-marketplace`:

```yaml
registries:
  - url: https://github.com/example-org/private-skills.git
    ref: main
```

Do not use `git config credential.helper store` or any other global Git mutation to satisfy `agent-marketplace`.

## Verify that credentials do not leak into the lock

The lock records only canonical origins, refs, commits, and digests. Confirm every `url:` in the lock is credential-free:

```bash
grep -E '^\s*url:' .agents/lock.yaml
```

Every line should show a bare `https://host/path.git` or `git@host:path.git` origin with no `user:token@` portion or query string. If you ever see one, the credential came from the configured registry URL: remove it from `.agents/config.yaml`, rely on the credential helper or SSH agent instead, and re-run `agent-marketplace lock`.

## Boundaries

- Never commit credentials to a registry URL, query parameter, or `~/.agents/config.yaml`.
- The lock contains exact commits, not credentials.
- Credential lifetime and rotation are the responsibility of the credential manager.

## Troubleshooting

- **`Authentication failed`**: confirm the credential helper or SSH key is current and has read access.
- **SSH URL does not resolve**: confirm the key is in the agent and the host is in `~/.ssh/known_hosts`.
