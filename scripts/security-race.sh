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

# fs-02: config layer authority and replacement races
# (internal/dependencytxn, internal/config): descriptor parent/leaf
# replacement barriers at capture, open, and publication for the
# dependency transaction and the config editor.
run_race ./internal/dependencytxn ./internal/config \
    -run '^Test(Config|Transaction)DescriptorParentReplacement$' \
    -count=20

run_race ./internal/fsboundary \
    -run '^Test(DirectoryRootAdmissionAndStrictPaths|CapturedDirectoryCreationCannotRedirectToReplacement)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(RejectsEscapingHarnessRoot|HarnessRootAliasesRemainUsable|UnsafeRootShapesPreserveState|UnsafeOutputAncestorPreservesState|RootAdmissionDryRunCreatesNoOutput|CreatesMissingMachineScope|CodexRelocationCreatesOnlyConfiguredPartition)$' \
    -count=20

run_race ./internal/fsboundary \
    -run '^TestNativeRootRetainsCapturedDirectoryAuthority$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(CacheLinkPublicationRefusesReplacedParent|CacheLinkPublicationRefusesReplacedLeaf|ManagedCacheLinksUpdateAndRepeat)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(MarkerMergeRefusesExternalAlias|ContainedMarkerAliasesPreserveNativeContent|ReadOnlyMarkerMergeRefusesExternalAlias|McpServerReplacesExistingSymlinkWithMergedFile)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(SupportCopyRefusesEscapingSourceLink|SupportCopyKeepsContainedAliasAndMode|SupportSourceRefusalPreservesInstalledGeneration)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(HookSupportSourceRefusalPreservesNativeContent|ContainedHookSupportAliasKeepsNativeContentAndMode|HookSourceRefusalKeepsIndependentNativeWork|GeminiWithoutHookSupportLaneDoesNotReadSources)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRun(PortableManifestAliasRefusalPreservesNativeOutput|ContainedPortableManifestParentAliasRetainsNativeUpdates|ReadOnlyPortableManifestParentRefusalIsNotAbsence|PortableManifestUnsafeShapesDoNotInstall)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRunPortableManifest(RemovalPreservesReplacedGeneration|PublicationPreservesReplacedGeneration|RepeatKeepsIdentityModeAndUpdates|StagingRefusesNamespaceReplacement|MissingParentDoesNotRecreate|EmptySaveRefusesAppearedLeaf)$' \
    -count=20

run_race ./internal/materialize \
    -run '^TestRunPortableSourceHash.*$' \
    -count=20

run_race ./internal/catalog ./internal/fsboundary \
    -run '^Test(LoadPrimarySource.*|SourceFSKeepsCapturedRootAndDirectoryInfo)$' \
    -count=20

run_race ./internal/sourcerevision \
    -run '^TestBuildSourceHash.*$' \
    -count=20

run_race ./internal/collection \
    -run '^TestDiscoverCollectionSource.*$' \
    -count=20

run_race ./cmd/agent-marketplace \
    -run '^TestConvertSource.*$' \
    -count=20

run_race ./internal/skill ./internal/fsboundary \
    -run '^Test(DiscoverLegacySourceMetadata.*|LegacySourceMetadata.*|TargetPrefixRead.*)$' \
    -count=20

run_race ./internal/tui \
    -run '^TestDetailSourcePreview.*$' \
    -count=20

run_race ./internal/materialize \
    -run '^Test(RunPortableOutputDigest.*|PortableRegularDigest.*)$' \
    -count=20
