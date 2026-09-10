#!/usr/bin/env bash
set -euo pipefail

if [[ "${HARNESS_TARGET:?}" == cursor ]]; then
  [[ "${CLI_VERSION:?}" =~ ^[0-9]{4}\.[0-9]{2}\.[0-9]{2}-[a-f0-9]{7,40}$ ]] || exit 2
else
  [[ "${CLI_VERSION:?}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+][A-Za-z0-9.-]+)?$ ]] || exit 2
fi
case "$HARNESS_TARGET" in
  claude) package=@anthropic-ai/claude-code; cli=claude ;;
  factory) package=droid; cli=droid ;;
  gemini) package=@google/gemini-cli; cli=gemini ;;
  codex) package=@openai/codex; cli=codex ;;
  cursor)
    test "${BUNDLE_URL:?}" = "https://downloads.cursor.com/lab/$CLI_VERSION/linux/x64/agent-cli-package.tar.gz" || exit 2
    mkdir -p /opt/cursor
    status=$(curl --proto '=https' --fail --silent --show-error --max-time 180 \
      --output /tmp/harness.tar.gz --write-out '%{http_code}' "$BUNDLE_URL")
    test "$status" = 200
    sha256sum /tmp/harness.tar.gz > /opt/cursor/bundle.sha256
    tar --strip-components=1 -xzf /tmp/harness.tar.gz -C /opt/cursor
    rm /tmp/harness.tar.gz
    test -x /opt/cursor/cursor-agent
    ln -s /opt/cursor/cursor-agent /usr/local/bin/agent
    exit 0
    ;;
  devin)
    [[ "${BUNDLE_SHA256:?}" =~ ^[a-f0-9]{64}$ ]] || exit 2
    [[ "${BUNDLE_URL:?}" == https://static.devin.ai/cli/* ]] || exit 2
    mkdir -p /opt/devin
    status=$(curl --proto '=https' --fail --silent --show-error --max-time 180 \
      --output /tmp/harness.tar.gz --write-out '%{http_code}' "$BUNDLE_URL")
    test "$status" = 200
    printf '%s  /tmp/harness.tar.gz\n' "$BUNDLE_SHA256" | sha256sum --check -
    tar -xzf /tmp/harness.tar.gz -C /opt/devin
    rm /tmp/harness.tar.gz
    test -x /opt/devin/bin/devin
    ln -s /opt/devin/bin/devin /usr/local/bin/devin
    exit 0
    ;;
  *) printf 'Unsupported harness target\n' >&2; exit 2 ;;
esac

test "${NPM_PACKAGE:?}" = "$package"
npm install --global --no-audit --no-fund "$package@$CLI_VERSION"
npm cache clean --force
test -x "/usr/local/bin/$cli"
