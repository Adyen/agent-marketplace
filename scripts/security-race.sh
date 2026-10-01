#!/usr/bin/env bash
# Checked contract race selection for the issue 215 security stack.
#
# .github/workflows/security-race.yml runs this script on Ubuntu at the
# exact pushed revision with Go 1.26.6 and CGO_ENABLED=1. Each run_race
# line below is one validation-contract selection; it is executed exactly
# as:
#
#   CGO_ENABLED=1 go test -race <packages...> -run '<regex>' -count <n>
#
# A selection is evidence only for the pushed SHA whose workflow run
# executed it. Add or update selections in the same commit as the code and
# tests they cover, and keep the local deterministic equivalent
# (CGO_ENABLED=0, identical selection) passing on macOS.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "security-race: revision $(git rev-parse HEAD)"
echo "security-race: $(go version)"
echo "security-race: CGO_ENABLED=${CGO_ENABLED:-<unset>}"

run_race() {
    echo "security-race: + CGO_ENABLED=1 go test -race $*"
    CGO_ENABLED=1 go test -race "$@"
}

# fs-01: shared descriptor-bound filesystem primitives
# (internal/fsboundary): leaf/parent replacement barriers and post-read
# identity stability.
run_race ./internal/fsboundary \
    -run '^Test(Descriptor(Leaf|Parent)ReplacementBarrier|ReadFilePostReadStability|ReadTargetRejectsReplacementAfterPrecheck|NestedOpenFailureClosesCapturedDirectories|PublishRejectsPathShapedStagedNames|ReadLimitCannotOverflow|ChainedLinkRetargetingRefuses|LinkResolutionRejectsNonLeadingParentComponents|ReadTargetDetectsRewriteWithRestoredMtime|LinkedStagingUsesEffectiveParent)$' \
    -count=20
