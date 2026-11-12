#!/usr/bin/env bash
set -euo pipefail

artifact_dir=${1:?artifact directory is required}
repo_root=${2:-$PWD}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
snapshot="$work/original-digests.json"
go run ./tools/gui-release-check snapshot --dir "$artifact_dir" --output "$snapshot"

case "$(uname -s)" in
  Darwin)
    archive=$(find "$artifact_dir" -maxdepth 1 -type f -name '*-macos-universal.zip' -print -quit)
    test -n "$archive"
    mkdir -p "$work/unpacked" "$work/copies"
    ditto -x -k "$archive" "$work/unpacked"
    cp -R "$work/unpacked/Agent Marketplace.app" "$work/copies/Agent Marketplace.app"
    codesign --force --deep --options runtime --entitlements "$repo_root/packaging/gui/macos/entitlements.plist" -s - "$work/copies/Agent Marketplace.app"
    codesign --verify --strict --deep "$work/copies/Agent Marketplace.app"
    ;;
  Linux)
    sudo apt-get update
    sudo apt-get install -y gnupg rpm
    export GNUPGHOME="$work/gnupg"
    mkdir -m 700 "$GNUPGHOME"
    key_uid='Agent Marketplace CI <gui-ci@example.invalid>'
    gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key "$key_uid" rsa2048 sign 0
    fingerprint=$(gpg --with-colons --list-secret-keys "$key_uid" | awk -F: '$1 == "fpr" { print $10; exit }')
    test -n "$fingerprint"
    mkdir "$work/rpmdb"
    rpm --dbpath "$work/rpmdb" --initdb
    gpg --armor --export "$fingerprint" > "$work/public-key.asc"
    rpm --dbpath "$work/rpmdb" --import "$work/public-key.asc"
    rpm_count=0
    for asset in "$artifact_dir"/*.rpm; do
      test -f "$asset" || continue
      rpm_count=$((rpm_count + 1))
      copy="$work/$(basename "$asset")"
      cp -f "$asset" "$copy"
      rpm --define "_gpg_path $GNUPGHOME" --define "_gpg_name $key_uid" --addsign "$copy"
      rpm --dbpath "$work/rpmdb" -K "$copy"
    done
    test "$rpm_count" -gt 0
    gpg --batch --yes --delete-secret-and-public-key "$fingerprint"
    gpgconf --kill all
    rm -rf "$GNUPGHOME"
    ;;
  *)
    echo "unsupported signing runner: $(uname -s)" >&2
    exit 1
    ;;
esac

go run ./tools/gui-release-check verify-digests --dir "$artifact_dir" --snapshot "$snapshot"
