#!/bin/sh
# Validate a release tag name before it becomes privileged build input.
#
# Release automation builds artifacts with VERSION taken from the pushed tag
# name, and Git accepts tag names containing shell and Make metacharacters
# (`v1.0.0$(id)`, `v1.0.0'; id; '`, ...). The tag is therefore untrusted input,
# and this script is the single admission gate shared by
# .github/workflows/ci.yml and the Makefile `release` target.
#
# Contract: v<MAJOR>.<MINOR>.<PATCH>[-<prerelease>]
#   - strict SemVer 2.0.0 core with a mandatory leading "v"
#   - no build metadata ("+..."), because the value is used verbatim in release
#     artifact filenames and in the release tag trigger glob
#
# Exit status: 0 accepted, 1 rejected, 2 usage error.

set -eu

# Pin byte-oriented semantics for every tool below. Under a UTF-8 locale, grep
# would parse the tag as multibyte text and tr's [:print:] class would follow
# the locale, so acceptance and redaction could differ between runners with
# different default locales; worse, tr aborts with "Illegal byte sequence" on
# invalid UTF-8 input, turning a rejectable tag into an uncontrolled set -e
# exit with no diagnostic. LC_ALL=C overrides every locale category, making
# the tag a byte string and the contract identical on every runner.
export LC_ALL=C

SEMVER_ERE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$'

if [ "$#" -ne 1 ]; then
	printf 'usage: %s <release-tag>\n' "$0" >&2
	exit 2
fi

tag=$1

fail() {
	printf 'error: rejected release tag (%s)\n' "$1" >&2
	printf 'hint: expected v<MAJOR>.<MINOR>.<PATCH>[-<prerelease>], e.g. v1.4.0 or v1.4.0-rc.1\n' >&2
	printf 'hint: see docs/release-process.md for the release tag contract\n' >&2
	exit 1
}

if [ -z "$tag" ]; then
	fail 'empty value'
fi

# Reject embedded newlines before anything else: every downstream check here is
# line oriented, so a rejected line could otherwise hide behind an accepted one.
if [ "$(printf '%s' "$tag" | wc -l | tr -d ' \t')" != "0" ]; then
	fail 'contains a newline'
fi

# Non-printable bytes are replaced before the value is echoed so a crafted tag
# cannot forge log output with terminal escape sequences.
printable=$(printf '%s' "$tag" | tr -c '[:print:]' '?')

if ! printf '%s\n' "$tag" | grep -Eq "$SEMVER_ERE"; then
	fail "not strict SemVer: $printable"
fi

printf 'accepted release tag: %s\n' "$printable"
