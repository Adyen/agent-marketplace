ARG BASE_IMAGE=oraclelinux:10@sha256:2f145af52abf6ee5be815b35751c65dc1531a4c709904c4fd7f72b23fdbc5015
FROM ${BASE_IMAGE}

USER root
ARG BASE_FLAVOR=oracle
ARG NODE_VERSION=24.18.0
RUN set -eu; \
    if [ "$BASE_FLAVOR" = oracle ]; then \
      arch="$(uname -m)"; \
      case "$arch" in \
        aarch64) nodearch=arm64; nodesha=58c9520501f6ae2b52d5b210444e24b9d0c029a58c5011b797bc1fe7105886f6 ;; \
        x86_64) nodearch=x64; nodesha=55aa7153f9d88f28d765fcdad5ae6945b5c0f98a36881703817e4c450fa76742 ;; \
        *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
      esac; \
      dnf install -y tar xz curl ca-certificates git procps-ng && dnf clean all; \
      archive="/tmp/node-v${NODE_VERSION}-linux-${nodearch}.tar.xz"; \
      curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${nodearch}.tar.xz" -o "$archive"; \
      printf '%s  %s\n' "$nodesha" "$archive" | sha256sum -c -; \
      mkdir -p /usr/local/node; \
      tar -C /usr/local/node --strip-components=1 -xJf "$archive"; \
      rm -f "$archive"; \
      ln -s /usr/local/node/bin/node /usr/local/bin/node; \
      ln -s /usr/local/node/bin/npm /usr/local/bin/npm; \
      ln -s /usr/local/node/bin/npx /usr/local/bin/npx; \
    elif [ "$BASE_FLAVOR" = debian-node ]; then \
      apt-get update; \
      apt-get install -y --no-install-recommends git procps curl ca-certificates; \
      rm -rf /var/lib/apt/lists/*; \
      [ "$(node --version)" = "v${NODE_VERSION}" ]; \
    else \
      echo "unsupported BASE_FLAVOR: $BASE_FLAVOR" >&2; exit 1; \
    fi

WORKDIR /workbench
COPY tools/tui-qa/package.json tools/tui-qa/package-lock.json ./
RUN --mount=type=secret,id=organization-ca,required=false \
    if [ -f /run/secrets/organization-ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/organization-ca; fi; \
    npm ci
COPY tools/tui-qa/tsconfig.json tools/tui-qa/tsconfig.tests.json tools/tui-qa/tsconfig.unit.json tools/tui-qa/tui-test.config.ts ./
COPY tools/tui-qa/scripts ./scripts
COPY tools/tui-qa/src ./src
COPY tools/tui-qa/tests ./tests
COPY tools/tui-qa/fixtures ./fixtures
RUN mkdir -p /etc/agent-marketplace
COPY tools/tui-qa/fixtures/organization-config.yaml /etc/agent-marketplace/config.yaml
COPY tools/tui-qa/schemas ./schemas
COPY tools/tui-qa/selftest-containment.sh ./selftest-containment.sh
COPY tools/tui-qa/container-entrypoint.sh /container-entrypoint.sh
RUN npm run build && chmod +x /container-entrypoint.sh /workbench/selftest-containment.sh /workbench/fixtures/*.sh

ENV AM_TUI_QA_ARTIFACT_DIR=/qa/artifacts
ENV AM_TUI_QA_BINARY=/opt/agent-marketplace
ENV AM_TUI_QA_BUILD_IDENTITY=/opt/build-identity.json
ENV AM_TUI_QA_REPO_ROOT=/src
ENV HOME=/sandbox/home
ENV TMPDIR=/sandbox

ENTRYPOINT ["/container-entrypoint.sh"]
