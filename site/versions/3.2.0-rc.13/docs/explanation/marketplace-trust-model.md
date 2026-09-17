# Marketplace trust model

A hosted Marketplace provides a catalog and immutable artifacts, but it does not implement user authentication or authorization itself. The deployment boundary provides trust.

## What the application trusts

- The operator configuration that lists approved registries.
- The Git origin and ref of each configured registry.
- The worker's content digest of an artifact.
- The revocation list in the operator configuration.

## What the application does not do

- Authenticate users.
- Authorize individual resources.
- Encrypt data at rest or in transit by itself.
- Manage secrets.

These are the responsibility of the deployment environment.

## Operator responsibilities

The deployment environment, not the application, provides authentication, authorization, encryption, and secret management. The operator is responsible for:

- the TLS-terminating, identity-aware reverse proxy in front of the API and UI;
- keeping the operations listener on a private network;
- restricting worker egress to approved Git and object-storage destinations;
- running containers as non-root users;
- managing DSN and registry credentials through a secret manager; and
- configuring license and content policy explicitly.

These are policy and infrastructure boundaries, not steps the CLI performs.

## Consumer trust

The consumer CLI verifies the Marketplace well-known document and `instance_id` on registration. It installs exact revisions by content digest. It respects revocation lists from the operator.

## Related documentation

- [Marketplace security](https://github.com/Adyen/agent-marketplace/blob/main/docs/marketplace-security.md)
- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [Marketplace architecture](./marketplace-architecture.md)
