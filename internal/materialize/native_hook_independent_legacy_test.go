package materialize

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"golang.org/x/sys/unix"
)

func independentlySatisfiedNativeHook(w skillTarget, repoRoot string, ins *nativeHookInspection) (bool, error) {
	if ins == nil {
		return false, nil
	}
	if ins.err != nil {
		return false, ins.err
	}
	if w.machineScope || w.skill.Resource == nil || w.skill.Resource.Kind() != ir.KindHook || harness.IsInstallationAdapterTarget(w.target) {
		return false, nil
	}
	name := w.skill.Name
	if w.skill.InstalledName != "" {
		name = w.skill.InstalledName
	}
	if ins.proof.HasHookResource(name) || len(ins.entries) == 0 {
		return false, nil
	}
	inspectedLayout, ok := nativeHookLayoutFor(w.target, false)
	if !ok {
		return false, nil
	}
	projection, err := prepareNativeHookProjection(w, repoRoot)
	if err != nil {
		if errors.Is(err, errNativeHookSourceUnavailable) {
			return false, nil
		}
		if coded, ok := diag.AsCodedError(err); ok {
			return false, coded
		}
		if nativeHookUnsafeBoundary(err) {
			return false, nativeHookRefused(ins.root, inspectedLayout.rel, err)
		}
		return false, nil
	}
	if projection.layout.rel != inspectedLayout.rel || projection.layout.container != inspectedLayout.container || projection.layout.requireVersion != inspectedLayout.requireVersion ||
		ins.root == "" || !sameOutputDir(ins.root, projection.outputRoot) {
		return false, nil
	}
	expected, err := nativeHookProjectionEntries(projection)
	if err != nil {
		if coded, ok := diag.AsCodedError(err); ok {
			return false, coded
		}
		return false, nil
	}
	if len(expected) == 0 {
		return false, nil
	}
	satisfactions := ins.proof.HookSatisfactions()
	for _, expectedEntry := range expected {
		for _, satisfaction := range satisfactions {
			if satisfaction.ID() == expectedEntry.value.ID {
				return false, nil
			}
		}
	}
	if !nativeHookEntryMultisetMatches(expected, ins.entries) {
		return false, nil
	}
	return nativeHookProjectionSupportMatches(projection, ins.root, projection.layout.rel)
}

func nativeHookProjectionEntries(projection nativeHookProjection) ([]ownedJSONExistingEntry, error) {
	if projection.layout.shape != targetmeta.HookShapeMatcherGroups && projection.layout.shape != targetmeta.HookShapeEventHandlers {
		return nil, errors.New("native hook projection has an unsupported shape")
	}
	var entries []ownedJSONExistingEntry
	for _, projected := range projection.artifacts {
		if err := projected.Validate(); err != nil {
			return nil, err
		}
		if projected.Path != projection.layout.rel {
			return nil, errors.New("native hook projection artifact does not match its declared file")
		}
		doc, err := jsonc.Parse(projected.Path, projected.Bytes)
		if err != nil {
			return nil, err
		}
		root := doc.Root()
		if root.Kind() != jsonc.KindObject {
			return nil, errors.New("native hook projection root is not a JSON object")
		}
		if err := nativeHookProjectionEnvelope(root, projection.layout); err != nil {
			return nil, err
		}
		container := root
		if projection.layout.container != "" {
			member, ok := root.Member(projection.layout.container)
			if !ok || member.Kind() != jsonc.KindObject {
				return nil, errors.New("native hook projection has no declared hook member")
			}
			container = member
		}
		current, err := nativeHookEntries(projected.Path, projection.layout, container)
		if err != nil {
			return nil, err
		}
		entries = append(entries, current...)
	}
	return entries, nil
}

func nativeHookProjectionEnvelope(root *jsonc.Node, layout nativeHookLayout) error {
	if layout.requireVersion {
		if err := nativeHookCheckVersion(root); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(layout.envelope))
	for key := range layout.envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		actual, ok := root.Member(key)
		if !ok {
			return fmt.Errorf("native hook projection is missing its declared %q envelope", key)
		}
		wantRaw, err := json.Marshal(layout.envelope[key])
		if err != nil {
			return errors.New("native hook projection has an invalid declared envelope")
		}
		want, err := canonicalOwnedJSON(wantRaw)
		if err != nil {
			return errors.New("native hook projection has an invalid declared envelope")
		}
		got, err := canonicalOwnedJSON(actual.Raw())
		if err != nil || !bytes.Equal(got, want) {
			return fmt.Errorf("native hook projection has a mismatched declared %q envelope", key)
		}
	}
	return nil
}

func nativeHookEntryMultisetMatches(expected []ownedJSONExistingEntry, actual []NativeHookEntry) bool {
	if len(expected) == 0 {
		return false
	}
	type entryKey struct {
		id          ownership.EntryID
		valueDigest string
	}
	want := make(map[entryKey]int, len(expected))
	for _, entry := range expected {
		want[entryKey{id: entry.value.ID, valueDigest: entry.value.ValueDigest}]++
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

func nativeHookProjectionSupportMatches(projection nativeHookProjection, refusedRoot, refusedFile string) (bool, error) {
	if len(projection.support) == 0 {
		return true, nil
	}
	if !filepath.IsAbs(projection.outputRoot) || !fs.ValidPath(projection.supportRelBase) || path.IsAbs(projection.supportRelBase) || path.Clean(projection.supportRelBase) != projection.supportRelBase {
		err := fmt.Errorf("%w: native hook support root is not a safe relative path", fsboundary.ErrUnsafePath)
		return false, nativeHookRefused(refusedRoot, refusedFile, err)
	}
	rootFD, err := unix.Open(projection.outputRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nativeHookSupportReadFailure(refusedRoot, refusedFile, err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	supportFD, err := openHookSupportDirAt(rootFD, projection.supportRelBase)
	if err != nil {
		return nativeHookSupportReadFailure(refusedRoot, refusedFile, err)
	}
	defer func() { _ = unix.Close(supportFD) }()
	var total int64
	seen := make(map[string]bool, len(projection.support))
	for _, expected := range projection.support {
		if !fs.ValidPath(expected.Path) || expected.Path == "." || path.IsAbs(expected.Path) || path.Clean(expected.Path) != expected.Path {
			err := fmt.Errorf("%w: native hook support file path is not safe", fsboundary.ErrUnsafePath)
			return false, nativeHookRefused(refusedRoot, refusedFile, err)
		}
		if seen[expected.Path] {
			return false, nil
		}
		seen[expected.Path] = true
		if err := expected.Validate(); err != nil {
			return false, nil
		}
		actual, mode, err := readHookSourceSnapshotFileNoAliases(supportFD, expected.Path, &total)
		if err != nil {
			return nativeHookSupportReadFailure(refusedRoot, refusedFile, err)
		}
		if !bytes.Equal(actual, expected.Bytes) || mode.Perm() != expected.Mode.Perm() {
			return false, nil
		}
	}
	return true, nil
}

func nativeHookSupportReadFailure(root, file string, err error) (bool, error) {
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return false, nil
	}
	if nativeHookUnsafeBoundary(err) {
		return false, nativeHookRefused(root, file, err)
	}
	return false, nil
}

func nativeHookUnsafeBoundary(err error) bool {
	return errors.Is(err, fsboundary.ErrUnsafePath) ||
		errors.Is(err, fsboundary.ErrUnsafeFile) ||
		errors.Is(err, fsboundary.ErrTooLarge) ||
		errors.Is(err, fsboundary.ErrGenerationConflict) ||
		errors.Is(err, dependencytxn.ErrGenerationConflict) ||
		errors.Is(err, unix.ELOOP) ||
		errors.Is(err, unix.ENOTDIR)
}

func preserveNativeSatisfiedLegacyTwins(key outputKey, legacyRoot string, artifacts []artifact.Artifact) ([]artifact.Artifact, error) {
	// A fresh backing array is required: an error return hands the caller's
	// original slice back, and reusing its storage would leak the
	// already-rewritten fragment bytes into that "unchanged" input.
	out := make([]artifact.Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if !a.Fragment {
			out = append(out, a)
			continue
		}
		events, nested, ok := hookFragmentEventMap(a.Bytes)
		if !ok {
			out = append(out, a)
			continue
		}
		legacyRel := a.Path
		if key.target == hcc.TargetWindsurf && key.machineScope {
			// The machine-scope install root already IS ~/.codeium/windsurf:
			// the compiled ".windsurf/" prefix is a project-scope prefix that
			// machine-scope plans strip before writing.
			legacyRel = strings.TrimPrefix(a.Path, windsurfDir+"/")
		}
		filtered := make(map[string][]json.RawMessage, len(events))
		for _, event := range sortedKeys(events) {
			var kept []json.RawMessage
			for _, rule := range events[event] {
				present, err := legacyManagedRulePresent(legacyRoot, legacyRel, nested, event, rule)
				if err != nil {
					return artifacts, err
				}
				if present {
					kept = append(kept, rule)
				}
			}
			if len(kept) > 0 {
				filtered[event] = kept
			}
		}
		if len(filtered) == 0 {
			// The hook is natively satisfied and no managed legacy twin
			// survives: it contributes nothing to the legacy file.
			continue
		}
		body, err := marshalHookFragment(filtered, nested)
		if err != nil {
			out = append(out, a)
			continue
		}
		a.Bytes = body
		out = append(out, a)
	}
	return out, nil
}
