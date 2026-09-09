ARG GO_IMAGE
ARG NODE_IMAGE
FROM ${GO_IMAGE} AS go
FROM ${NODE_IMAGE}

COPY --from=go /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}" \
    CGO_ENABLED=0 \
    DISABLE_AUTOUPDATER=1 \
    FACTORY_DROID_AUTO_UPDATE_ENABLED=false

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git jq bsdutils \
    && rm -rf /var/lib/apt/lists/*

ARG HARNESS_TARGET
ARG CLI_VERSION
ARG NPM_PACKAGE
ARG BUNDLE_URL
ARG BUNDLE_SHA256
COPY install.sh /opt/harness-install.sh
RUN bash /opt/harness-install.sh

USER node
WORKDIR /workspace
ENTRYPOINT []
CMD ["bash"]
