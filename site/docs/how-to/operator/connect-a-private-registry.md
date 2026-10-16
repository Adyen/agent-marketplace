# How to connect a private registry

Configure a hosted Marketplace to clone a private Git registry over HTTPS.

These steps use the `registries:` list in the operator configuration, which applies with `management.mode: file` (the default). In `file` mode a registry `origin` must be an HTTP or HTTPS URL, so the worker cannot clone over SSH. To use an SSH origin (`ssh://git@host/path`), use database-managed administration with a credential profile under `management.profiles`; see [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md).

## Prerequisites

- A Marketplace operator configuration file.
- A private registry you can clone over HTTPS with a token.

## 1. Add a token credential reference

Add a `token` credential that points to a file or an environment variable:

```yaml
registries:
  - id: private-agents
    slug: private-agents
    origin: https://github.com/example-org/private-agents.git
    ref: main
    roots:
      - path: .
    credentials:
      token:
        file: /run/secrets/registry-token
```

To read the token from an environment variable instead, use `env: <VARIABLE_NAME>` in place of `file`. The worker sends the token to Git only for requests to that registry's origin.

The configuration also accepts an `ssh_key` credential, which cannot be combined with `token`:

```yaml
    credentials:
      ssh_key:
        file: /run/secrets/registry-ssh-key
```

The worker passes the key to `ssh` (with `BatchMode=yes`) only when Git uses the SSH transport.

## 2. Trust a private certificate authority (optional)

If the Git server uses a certificate from a private certificate authority, set `ca_file` on the registry to an absolute path of a readable PEM bundle. The path must be a regular file, not a symlink.

```yaml
registries:
  - id: private-agents
    # ...
    ca_file: /etc/agent-marketplace/ca/internal.pem
```

The worker uses this bundle only for Git commands against this registry.

## 3. Mount the secrets

Mount the token file read-only into the worker container, and into any container where you run `serve reindex`, because both clone the registry. For an `env` reference, set the variable in those containers.

Mount the `ca_file` read-only at the same path in every API and worker container. Every role validates the operator configuration at startup, and that validation reads the CA file.

## 4. Allow network egress

The worker must be able to reach the registry origin. Limit the worker's outbound access to approved hosts.

## 5. Reindex the registry

```bash
agent-marketplace serve reindex private-agents --config /app/marketplace.yaml
```

The command prints nothing on success and exits `0`; the [log file](../../reference/logging.md) records the refresh and ends with `reindex complete`. The worker also refreshes every configured registry on startup and at each `refresh.interval`, so this step only forces an immediate refresh.

## 6. Verify

Request a resource that you know exists in the registry:

```bash
curl -fsS "https://marketplace.example.com/api/v1/resources/private-agents/skill/review" -o revision.json \
  && python3 -c 'import json; r=json.load(open("revision.json")); print(r["coordinate"], r["content_digest"])'
```

A `404` means the worker has not published that resource yet. Check the worker log for acquisition errors before you retry.

## Boundaries

- The configuration fails to load if a credential contains an inline `value` field.
- Origins must not contain URL user information or credential query parameters.
- Git errors and logged origins are redacted, so the credential does not appear in logs or diagnostics.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
