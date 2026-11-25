package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"golang.org/x/sys/unix"
)

type forwardNativeHookCacheKey struct {
	target       outputKey
	locationPath string
}

type forwardNativeHookCacheEntry struct {
	root       string
	inspection *nativeHookInspection
	err        error
}

type forwardNativeHookCache struct {
	entries map[forwardNativeHookCacheKey]forwardNativeHookCacheEntry
}

func newForwardNativeHookCache() *forwardNativeHookCache {
	return &forwardNativeHookCache{entries: make(map[forwardNativeHookCacheKey]forwardNativeHookCacheEntry)}
}

// forwardNativeHookSatisfied checks declared repository-scope ForwardNative
// locations read-only for complete satisfaction of one selected hook (either
// via clean durable ownership/satisfaction proof matching resourceName and
// plan.sourceDigest, or via complete source-derived independent satisfaction
// together with verified regular support files).
//
// It never writes hook files, support files, or ownership state. Malformed,
// duplicate-key, or unsafe forward files fail closed with a coded error.
func forwardNativeHookSatisfied(key outputKey, repoRoot, resourceName string, plan hookSupportPlan, desired []ownedJSONEntry, cache *forwardNativeHookCache) (bool, error) {
	if key.machineScope || len(desired) == 0 || plan.sourceDigest == "" {
		return false, nil
	}
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), false)
	if err != nil || len(declaration.file.ForwardNative) == 0 {
		return false, nil
	}
	destination, _, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return false, nil
	}
	for _, loc := range declaration.file.ForwardNative {
		forwardRel := path.Join(declaration.partition.Prefix, loc.Path)
		ins, root, inspectErr := inspectForwardNativeLocation(key, destination.Root, forwardRel, loc, declaration.aggregate.Shape, cache)
		if inspectErr != nil {
			return false, inspectErr
		}
		if ins == nil || len(ins.entries) == 0 {
			continue
		}
		matchedEntries := false
		if forwardDurableProofSatisfies(ins, resourceName, plan.sourceDigest, desired) {
			matchedEntries = true
		} else if !ins.proof.HasHookResource(resourceName) && forwardIndependentEntriesMatch(desired, ins.entries, ins.proof) {
			matchedEntries = true
		}
		if !matchedEntries {
			continue
		}
		supportOK, supportErr := forwardNativeSupportMatches(root, forwardRel, plan.artifacts)
		if supportErr != nil {
			return false, supportErr
		}
		if supportOK {
			return true, nil
		}
	}
	return false, nil
}

func inspectForwardNativeLocation(key outputKey, outDir, rel string, loc targetmeta.NativeAggregateLocation, shape targetmeta.HookShape, cache *forwardNativeHookCache) (*nativeHookInspection, string, error) {
	cacheKey := forwardNativeHookCacheKey{target: key, locationPath: rel}
	if cache != nil {
		if cached, ok := cache.entries[cacheKey]; ok {
			return cached.inspection, cached.root, cached.err
		}
	}
	record := func(ins *nativeHookInspection, root string, err error) (*nativeHookInspection, string, error) {
		if cache != nil {
			cache.entries[cacheKey] = forwardNativeHookCacheEntry{root: root, inspection: ins, err: err}
		}
		return ins, root, err
	}
	canonicalRoot, err := ownership.CanonicalRoot(outDir)
	if err != nil {
		return record(nil, outDir, nativeHookRefused(outDir, rel, err))
	}
	store, err := ownership.Open(canonicalRoot)
	if err != nil {
		return record(nil, canonicalRoot, nativeHookRefused(canonicalRoot, rel, err))
	}
	layout := nativeHookLayout{
		rel:       rel,
		container: loc.Member,
		shape:     shape,
		envelope:  loc.Envelope,
	}
	if _, hasVersion := loc.Envelope["version"]; hasVersion {
		layout.requireVersion = true
	}
	ins, err := inspectNativeHookLayout(store, canonicalRoot, layout)
	if err != nil {
		return record(nil, canonicalRoot, err)
	}
	return record(ins, canonicalRoot, nil)
}

func forwardDurableProofSatisfies(ins *nativeHookInspection, resourceName, sourceDigest string, desired []ownedJSONEntry) bool {
	if ins == nil || resourceName == "" || sourceDigest == "" {
		return false
	}
	matchingOwned := 0
	for _, id := range ins.proof.OwnedIDs() {
		if id.Kind != ownership.KindHook || !ins.proof.HasHookSource(id, resourceName, sourceDigest) {
			continue
		}
		currentDigest, ok := ins.current[id]
		if !ok || ins.proof.Classify(id, currentDigest) != ownership.StatusOwnedClean {
			return false
		}
		matchingOwned++
	}
	matchingSatisfactions := 0
	for _, sat := range ins.proof.HookSatisfactions() {
		matchedSource := false
		for _, src := range sat.Sources {
			if src.Resource == resourceName && src.SourceDigest == sourceDigest {
				matchedSource = true
				break
			}
		}
		if !matchedSource {
			continue
		}
		currentDigest, ok := ins.current[sat.ID()]
		if !ok || currentDigest != sat.ValueDigest {
			return false
		}
		matchingSatisfactions++
	}
	if matchingOwned == 0 && matchingSatisfactions == 0 {
		return false
	}
	// Every desired entry must also be accounted for by a current entry on disk.
	for _, entry := range desired {
		if currentDigest, ok := ins.current[entry.id()]; !ok || currentDigest != entry.valueDigest() {
			return false
		}
	}
	return true
}

func forwardIndependentEntriesMatch(desired []ownedJSONEntry, actual []NativeHookEntry, proof ownership.Proof) bool {
	if len(desired) == 0 {
		return false
	}
	type entryKey struct {
		id          ownership.EntryID
		valueDigest string
	}
	satisfactions := proof.HookSatisfactions()
	want := make(map[entryKey]int, len(desired))
	for _, entry := range desired {
		id := entry.id()
		for _, sat := range satisfactions {
			if sat.ID() == id {
				return false
			}
		}
		want[entryKey{id: id, valueDigest: entry.valueDigest()}]++
	}
	got := make(map[entryKey]int, len(want))
	for _, entry := range actual {
		key := entryKey{id: entry.ID, valueDigest: entry.ValueDigest}
		if _, matches := want[key]; !matches {
			continue
		}
		if entry.Status != ownership.StatusUnmanaged {
			return false
		}
		got[key]++
	}
	for key, count := range want {
		if got[key] != count {
			return false
		}
	}
	return true
}

func forwardNativeSupportMatches(outputRoot, refusedFile string, support []artifact.Artifact) (bool, error) {
	if len(support) == 0 {
		return true, nil
	}
	if !filepath.IsAbs(outputRoot) {
		err := fmt.Errorf("%w: native hook output root is not absolute", fsboundary.ErrUnsafePath)
		return false, nativeHookRefused(outputRoot, refusedFile, err)
	}
	rootFD, err := unix.Open(outputRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return forwardSupportReadFailure(outputRoot, refusedFile, err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	var total int64
	seen := make(map[string]bool, len(support))
	for _, expected := range support {
		rel := path.Clean(expected.Path)
		if !fs.ValidPath(rel) || rel == "." || path.IsAbs(rel) || rel != expected.Path {
			err := fmt.Errorf("%w: native hook support file path is not safe", fsboundary.ErrUnsafePath)
			return false, nativeHookRefused(outputRoot, refusedFile, err)
		}
		if seen[rel] {
			return false, nil
		}
		seen[rel] = true
		if err := expected.Validate(); err != nil {
			return false, nil
		}
		dirRel := path.Dir(rel)
		base := path.Base(rel)
		openDirFD := rootFD
		var closeDirFD func()
		if dirRel != "." {
			dirFD, openErr := openHookSupportDirAt(rootFD, dirRel)
			if openErr != nil {
				return forwardSupportReadFailure(outputRoot, refusedFile, openErr)
			}
			openDirFD = dirFD
			closeDirFD = func() { _ = unix.Close(dirFD) }
		}
		actual, mode, readErr := readHookSourceSnapshotFileNoAliases(openDirFD, base, &total)
		if closeDirFD != nil {
			closeDirFD()
		}
		if readErr != nil {
			return forwardSupportReadFailure(outputRoot, refusedFile, readErr)
		}
		if !bytes.Equal(actual, expected.Bytes) || mode.Perm() != expected.Mode.Perm() {
			return false, nil
		}
	}
	return true, nil
}

func forwardSupportReadFailure(root, file string, err error) (bool, error) {
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
		return false, nil
	}
	if coded, ok := diag.AsCodedError(err); ok {
		return false, coded
	}
	return false, nativeHookRefused(root, file, err)
}

func nativeHookFilesExistOnDisk(key outputKey, repoRoot string) bool {
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return true
	}
	if _, err := os.Lstat(filepath.Join(destination.Root, filepath.FromSlash(file.rel))); err == nil || !os.IsNotExist(err) {
		return true
	}
	declaration, declErr := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if declErr != nil {
		return true
	}
	for _, legacy := range declaration.file.Legacy {
		rel := path.Join(declaration.partition.Prefix, legacy)
		if _, err := os.Lstat(filepath.Join(destination.Root, filepath.FromSlash(rel))); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	for _, retired := range declaration.file.RetiredNative {
		rel := path.Join(declaration.partition.Prefix, retired.Path)
		if _, err := os.Lstat(filepath.Join(destination.Root, filepath.FromSlash(rel))); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
