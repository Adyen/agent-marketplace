# How to connect a private registry

Configure a hosted Marketplace to clone a private Git registry.

## Prerequisites

- A Marketplace operator configuration file.
- A private registry you can clone over SSH or HTTPS.

## 1. Use a credential reference

For an HTTPS token, add a `token` credential that points to a file or environment variable:

```yaml
registries:
  - id: private-agents
    slug: private-agents
    origin: https://github.com/example-org/private-agents.git
    ref: main
    credentials:
      token:
        file: /run/secrets/registry-token
```

For an SSH key, use an `ssh_key` credential:

```yaml
    credentials:
      ssh_key:
        file: /run/secrets/registry-ssh-key
```

## 2. Mount the secret

Mount the credential file read-only into the worker container. `token` and `ssh_key` are mutually exclusive.

## 3. Validate network egress

The worker must reach the registry origin. Restrict the worker's outbound access to approved hosts.

## 4. Reindex the registry

```bash
agent-marketplace serve reindex private-agents --config /app/marketplace.yaml
```

## 5. Verify

Search the Marketplace for a known resource:

```bash
curl -fs \
  "https://marketplace.example.com/api/v1/resources/private-agents/skill/review" | head
```

## Boundaries

- Inline secret `value` fields are rejected.
- The worker uses the credential only for that registry and does not write it to logs or diagnostics.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
