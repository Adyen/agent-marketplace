# SkillSpector runner image

The runner image is built from the checked-in protocol server and an externally
vetted offline wheelhouse. No Marketplace runner image is prepublished by this
repository. Do not build it from a package index or an unverified tarball.

Stage `requirements.lock` and the exact package wheels in `wheelhouse/` from an
approved offline artifact source. The build verifies the SHA-256 of the
SkillSpector 2.13.6 wheel, installs only hashed wheels with networking disabled,
checks the installed dependency graph and exact package version, then copies
only the virtual environment into the final image.

Build with an approved immutable Python base reference:

```bash
docker build --network=none \
  --build-arg PYTHON_IMAGE=<verified-base-image@sha256:digest> \
  --tag agent-marketplace-skillspector:local \
  deploy/skillspector
```

The default `python:3.12-slim` reference is for local development only. The
`PYTHON_IMAGE` build argument may name an approved mirror or an already-cached
test base. Compose does not build or pull the runner. Build or load a verified
image before starting either Compose profile. Its default reference is the
local tag shown above. Production Compose and Helm values can use an
operator-controlled checked image digest.
The process starts as UID/GID 1000. The Compose and Helm deployments enforce a
read-only root; the image itself does not. It listens on the scoped Unix socket
by default, and Helm overrides this to the private TCP listener on port 8081.

## Verify a real scan

Build or load the candidate Marketplace worker and runner images. Run the
SQLite Compose witness with their local names or digests:

```bash
make skillspector-witness \
  AM_SKILLSPECTOR_APP_IMAGE=agent-marketplace-worker:local \
  AM_SKILLSPECTOR_RUNNER_IMAGE=agent-marketplace-skillspector:local
```

The witness never pulls images. It checks runner isolation and requires a
published skill to contain the real partial report without changing the
built-in policy decision. A skipped witness fails the target.
