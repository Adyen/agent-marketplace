#!/usr/bin/env bash
set -euo pipefail

target=${1:?harness target required}
image=${2:?image reference required}
publish=${3:-}
[[ $# -le 3 && ( -z "$publish" || "$publish" == --push ) ]] || exit 2
[[ "$image" =~ ^ghcr\.io/[a-z0-9._/-]+:ci$ ]] || exit 2
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
artifacts=${HARNESS_COMPAT_REPORT_DIR:?report directory required}
mkdir -p "$artifacts"
go run ./tools/harness-compat resolve --target "$target" > "$artifacts/release.json"
version=$(jq -er .version "$artifacts/release.json")
cli=$(jq -er .cli "$artifacts/release.json")
package=$(jq -r .npm_package "$artifacts/release.json")
bundle=$(jq -r .bundle_url "$artifacts/release.json")
checksum=$(jq -r .bundle_sha256 "$artifacts/release.json")
go_version=$(GOWORK=off go list -m -f '{{.GoVersion}}')

for base in "golang:${go_version}-bookworm" node:24-bookworm-slim; do
  docker pull --platform linux/amd64 "$base"
done
go_image=$(docker image inspect "golang:${go_version}-bookworm" --format '{{index .RepoDigests 0}}')
node_image=$(docker image inspect node:24-bookworm-slim --format '{{index .RepoDigests 0}}')
recipe=$(sha256sum tools/harness-compat/Dockerfile tools/harness-compat/.dockerignore tools/harness-compat/install.sh tools/harness-compat/build-image.sh | sha256sum | cut -d ' ' -f 1)
created=$(date -u +%Y-%m-%dT%H:%M:%SZ)
revision=$(git rev-parse HEAD)
local_image="agent-marketplace-harness:$target"
source_url="https://github.com/${GITHUB_REPOSITORY:?}"

docker build --platform linux/amd64 --pull \
  --build-arg "GO_IMAGE=$go_image" --build-arg "NODE_IMAGE=$node_image" \
  --build-arg "HARNESS_TARGET=$target" --build-arg "CLI_VERSION=$version" \
  --build-arg "NPM_PACKAGE=$package" --build-arg "BUNDLE_URL=$bundle" \
  --build-arg "BUNDLE_SHA256=$checksum" \
  --label "org.opencontainers.image.source=$source_url" \
  --label "org.opencontainers.image.revision=$revision" \
  --label "org.opencontainers.image.created=$created" \
  --label "org.opencontainers.image.version=$version" \
  --label "io.agent-marketplace.target=$target" \
  --label "io.agent-marketplace.cli=/usr/local/bin/$cli" \
  --label "io.agent-marketplace.go-version=$go_version" \
  --label "io.agent-marketplace.recipe-sha=$recipe" \
  --label "io.agent-marketplace.go-image=$go_image" \
  --label "io.agent-marketplace.node-image=$node_image" \
  --tag "$local_image" tools/harness-compat

version_output=$(docker run --rm --init --network none --cap-drop ALL \
  --security-opt no-new-privileges --env HOME=/tmp/harness-smoke \
  "$local_image" "/usr/local/bin/$cli" --version)
printf '%s\n' "$version_output"
if [[ "$target" == cursor ]]; then
  [[ "$version_output" =~ ([0-9]{4}\.[0-9]{2}\.[0-9]{2}-[a-f0-9]{7,40}) ]] || exit 1
else
  [[ "$version_output" =~ ([0-9]+\.[0-9]+\.[0-9]+([+][A-Za-z0-9.-]+)?) ]] || exit 1
fi
test "${BASH_REMATCH[1]}" = "$version"

if [[ "$publish" == --push ]]; then
  test "${GITHUB_EVENT_NAME:-}" != pull_request
  immutable="${image%:ci}:${version//+/-}-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
  docker tag "$local_image" "$immutable"
  docker push "$immutable"
  docker tag "$local_image" "$image"
  docker push "$image"
fi

docker image inspect "$local_image" > "$artifacts/image.json"
image_id=$(docker image inspect "$local_image" --format '{{.Id}}')
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  printf 'image_id=%s\nversion=%s\ncreated=%s\n' "$image_id" "$version" "$created" >> "$GITHUB_OUTPUT"
fi
