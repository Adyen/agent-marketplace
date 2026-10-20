#!/usr/bin/env bash
set -euo pipefail

main() {
  repo=Adyen/agent-marketplace
  fail() {
    printf 'agent-marketplace install: %s\n' "$*" >&2
    exit 1
  }

  system=$(uname -s)
  case "$system" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) fail "unsupported operating system: $system" ;;
  esac

  machine=$(uname -m)
  case "$machine" in
    amd64|x86_64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) fail "unsupported processor architecture: $machine" ;;
  esac

  tag=${AGENT_MARKETPLACE_VERSION:-}
  if [[ -n "$tag" ]]; then
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "invalid version: $tag"
  else
    latest_url=$(curl -fsSLI --proto '=https' -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest" 2>/dev/null) ||
      fail "could not resolve latest release"
    case "$latest_url" in
      */releases/tag/*) tag=${latest_url##*/} ;;
      *) fail "latest release did not resolve to a stable tag" ;;
    esac
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "latest release resolved to an invalid tag: $tag"
  fi

  home=${HOME:-}
  [[ -n "$home" ]] || fail "HOME is not set"
  install_dir=${AGENT_MARKETPLACE_INSTALL_DIR:-"$home/.local/bin"}
  install_dir=${install_dir%/}
  asset="agent-marketplace-$tag-$os-$arch"
  base_url="https://github.com/$repo/releases/download/$tag"
  workdir=$(mktemp -d 2>/dev/null) || fail "could not create a temporary directory"
  install_tmp=
  cleanup() {
    if [[ -n "$install_tmp" ]]; then rm -f "$install_tmp"; fi
    rm -rf "$workdir"
  }
  trap cleanup EXIT

  if ! curl -fsSL --proto '=https' -o "$workdir/$asset" "$base_url/$asset" 2>/dev/null; then
    fail "download failed: $base_url/$asset"
  fi
  if ! curl -fsSL --proto '=https' -o "$workdir/SHA256SUMS" "$base_url/SHA256SUMS" 2>/dev/null; then
    fail "download failed: $base_url/SHA256SUMS"
  fi
  if ! expected=$(awk -v filename="$asset" '$2 == filename { print $1; found=1; exit } END { if (!found) exit 1 }' "$workdir/SHA256SUMS" 2>/dev/null); then
    fail "SHA256SUMS has no entry for $asset"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$workdir/$asset" 2>/dev/null) || fail "could not calculate SHA-256 for $asset"
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$workdir/$asset" 2>/dev/null) || fail "could not calculate SHA-256 for $asset"
  else
    fail "sha256sum or shasum is required to verify $asset"
  fi
  actual=${actual%% *}
  [[ "$actual" == "$expected" ]] || fail "SHA-256 mismatch for $asset"

  if ! mkdir -p "$install_dir" 2>/dev/null; then
    fail "could not create install directory: $install_dir"
  fi
  install_tmp=$(mktemp "$install_dir/.agent-marketplace.XXXXXX" 2>/dev/null) ||
    fail "could not create a temporary file in $install_dir"
  if ! cp "$workdir/$asset" "$install_tmp" 2>/dev/null; then
    fail "could not copy verified binary to $install_dir"
  fi
  if ! chmod 0755 "$install_tmp" 2>/dev/null; then
    fail "could not make installed binary executable"
  fi
  if ! mv -f "$install_tmp" "$install_dir/agent-marketplace" 2>/dev/null; then
    fail "could not install binary in $install_dir"
  fi
  install_tmp=

  "$install_dir/agent-marketplace" --version
  case ":${PATH:-}:" in
    *":$install_dir:"*) ;;
    *) printf "Install directory %s is not on PATH.\nAdd it with:\nexport PATH=\"%s:\$PATH\"\n" "$install_dir" "$install_dir" ;;
  esac
}
main "$@"
