## Context

Keep the existing non-root/read-only deployment controls and generic mirror
overrides. Do not hard-code unverified application digests or organization
registry names into public defaults.

## Goals / Non-Goals

Goals: direct scanner failure, no implicit Helm image version, a shell-free
Node runtime, and explicit production digest-pinning guidance.

Non-goals: Kubernetes admission policy, registry-signature tooling, automated
pin rotation, application changes, or forbidding explicit development tags.

## Decisions

- Remove scanner `continue-on-error` and its redundant failure step. Keep PR
  and merge-group triggers and the pinned scanner action.
- Leave Helm tags empty and require a tag when no digest is supplied in the
  shared image helper. Preserve role inheritance and digest precedence;
  frontends carry independent versions.
- Build Node output on Debian 13, matching the distroless Node 24 runtime's
  libc family. Introduce `NODE_RUNTIME_IMAGE` independently of `NODE_IMAGE`.
  Use exec-form startup and healthchecks with `/nodejs/bin/node`.
- Preserve overridable public development defaults. Require production
  operators to verify and pin both base images and deployed images by digest,
  even in internal mirrors. Do not invent digests before registry verification.

## Risks / Trade-offs

Helm consumers relying on implicit `latest` must supply a version. Mirror users
must set the new runtime override as well as the builder override, and their
runtime must retain the documented Node path and ABI. Digest guidance is not a
substitute for admission controls or a registry trust policy. Scanner failure
can prevent merges only when the corresponding check is required by repository
branch protection.

## Validation

Regression tests fail against the prior scanner configuration, runtime
Dockerfile, and Helm defaults. The Helm gate renders absent-version refusals,
explicit development tags, digest precedence, frontend isolation, and split
role overrides. Container CI starts the real web image non-root/read-only at
both root and a subpath and checks health plus the absence of shell/package
manager executables. Run scoped Go tests, container/Compose checks, the full
existing Helm validation script, spec-check, and make validate locally.
