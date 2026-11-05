package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	nativeHookMetadataFPAlpha = "sha256:" + strings.Repeat("a", 64)
	nativeHookMetadataFPBeta  = "sha256:" + strings.Repeat("b", 64)
	nativeHookMetadataFPGamma = "sha256:" + strings.Repeat("c", 64)
	nativeHookMetadataFPDelta = "sha256:" + strings.Repeat("d", 64)
)

type nativeHookMetadataContributor struct {
	resource    string
	fingerprint string
}

func nativeHookMetadataWork(t *testing.T, tc nativeHookJSONLayoutCase, command string, contributors []nativeHookMetadataContributor) *ownedJSONAggregateWork {
	t.Helper()
	entries, err := ownedJSONHookEntries(tc.rel, map[string][]json.RawMessage{
		tc.event: {json.RawMessage(nativeHookMetadataRule(t, tc, command))},
	}, contributors[0].resource)
	if err != nil {
		t.Fatalf("construct desired native hook entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one desired entry, got %d", len(entries))
	}
	work := &ownedJSONAggregateWork{}
	for _, contributor := range contributors {
		entry := entries[0]
		entry.resource = contributor.resource
		entry.sourceDigest = contributor.fingerprint
		work.add(entry)
	}
	return work
}

func nativeHookMetadataRule(t *testing.T, tc nativeHookJSONLayoutCase, command string) string {
	t.Helper()
	switch tc.shape {
	case targetmeta.HookShapeMatcherGroups:
		return nativeHookJSONGroup(command, tc.matcher, "")
	case targetmeta.HookShapeEventHandlers:
		return nativeHookJSONFlatHandler(command, tc.matcher)
	default:
		t.Fatal("unsupported native hook test shape")
		return ""
	}
}

func nativeHookMetadataTarget(t *testing.T, tc nativeHookJSONLayoutCase, repoRoot, home, codexHome string) (outputKey, string, ownedJSONFileSpec, string) {
	t.Helper()
	key := outputKey{target: tc.target, machineScope: tc.machineScope}
	destination, spec, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		t.Fatal("native hook layout did not resolve")
	}
	wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
	if err != nil {
		t.Fatal("expected root could not be canonicalized")
	}
	gotRoot, err := ownership.CanonicalRoot(destination.Root)
	if err != nil || gotRoot != wantRoot || spec.rel != tc.rel {
		t.Fatal("native hook layout differs from the independent expectation")
	}
	return key, wantRoot, spec, filepath.Join(wantRoot, filepath.FromSlash(tc.rel))
}

func nativeHookMetadataStore(t *testing.T, root string) *ownership.Store {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("open native ownership store: %v", err)
	}
	return store
}

func nativeHookMetadataProof(t *testing.T, store *ownership.Store, rel string) ownership.Proof {
	t.Helper()
	proof, err := store.Proof(rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	return proof
}

func runNativeHookMetadataReconcile(t *testing.T, key outputKey, repoRoot string, work *ownedJSONAggregateWork, dryRun bool) *Report {
	t.Helper()
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), dryRun)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("native hook reconciliation failed: %+v", report)
	}
	return report
}

// spec: hook-native-location-tolerance/satisfaction-proof-matches-every-contributor-exactly
func TestNativeHookMetadataHandwrittenTwinAcrossLayouts(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
			seed, handwritten, other, unrelated := nativeHookJSONIdenticalSeed(tc)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, seed, 0o640); err != nil {
				t.Fatal(err)
			}
			seedNativeHookJSONLock(t, wantRoot)
			work := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
				{resource: "beta", fingerprint: nativeHookMetadataFPBeta},
				{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
			})
			nativeBytes, nativeMode, nativeMtime := fileSnapshot(t, path)
			report := runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
			if !diag.HasCode(report.Diags, diag.CodeMaterializeHookSatisfiedByUnmanaged) {
				t.Fatal("handwritten twin was not reported satisfied")
			}
			assertFileUnchanged(t, path, nativeBytes, nativeMode, nativeMtime)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(handwritten)) ||
				!bytes.Contains(data, []byte(other)) || (unrelated != "" && !bytes.Contains(data, []byte(unrelated))) ||
				bytes.Contains(data, []byte("_managed_by")) {
				t.Fatal("handwritten twin aggregate changed or gained a marker")
			}
			store := nativeHookMetadataStore(t, wantRoot)
			proof := nativeHookMetadataProof(t, store, spec.rel)
			if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
				t.Fatalf("handwritten twin was adopted: %d/%v", proof.OwnedCount(), proof.OwnedIDs())
			}
			records := proof.HookSatisfactions()
			if len(records) != 1 {
				t.Fatalf("satisfaction records = %+v", records)
			}
			wantSources := []ownership.HookSourceEvidence{
				{Resource: "alpha", SourceDigest: nativeHookMetadataFPAlpha},
				{Resource: "beta", SourceDigest: nativeHookMetadataFPBeta},
			}
			if !reflect.DeepEqual(records[0].Sources, wantSources) {
				t.Fatalf("satisfaction sources = %+v, want %+v", records[0].Sources, wantSources)
			}
			if proof.Classify(records[0].ID(), records[0].ValueDigest) != ownership.StatusUnmanaged {
				t.Fatal("satisfaction granted ownership authority")
			}

			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
			assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)

			runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
			proof = nativeHookMetadataProof(t, store, spec.rel)
			if len(proof.HookSatisfactions()) != 0 {
				t.Fatalf("deselected satisfaction retained: %+v", proof.HookSatisfactions())
			}
			data, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, seed) {
				t.Fatal("deselection rewrote handwritten bytes")
			}
		})
	}
}

func TestNativeHookMetadataOwnedContributorRefreshAcrossLayouts(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
			alphaBeta := []nativeHookMetadataContributor{
				{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
				{resource: "beta", fingerprint: nativeHookMetadataFPBeta},
			}
			runNativeHookMetadataReconcile(t, key, repoRoot,
				nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, alphaBeta), false)
			store := nativeHookMetadataStore(t, wantRoot)
			proof := nativeHookMetadataProof(t, store, spec.rel)
			if proof.OwnedCount() != 1 {
				t.Fatalf("seed reconcile owned %d entries", proof.OwnedCount())
			}
			ownedID := proof.OwnedIDs()[0]
			if !proof.HasHookSource(ownedID, "beta", nativeHookMetadataFPBeta) {
				t.Fatalf("seed state lacks contributor beta evidence: %+v", proof)
			}

			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
			stateBefore, _, _ := fileSnapshot(t, store.StateFile())
			alphaOnly := []nativeHookMetadataContributor{
				{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
			}
			runNativeHookMetadataReconcile(t, key, repoRoot,
				nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, alphaOnly), false)
			assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
			stateAfter, _, _ := fileSnapshot(t, store.StateFile())
			if bytes.Equal(stateBefore, stateAfter) {
				t.Fatal("removing contributor B left stale satisfaction/entry evidence")
			}
			proof = nativeHookMetadataProof(t, store, spec.rel)
			if proof.OwnedCount() != 1 ||
				proof.HasHookSource(ownedID, "beta", nativeHookMetadataFPBeta) ||
				!proof.HasHookSource(ownedID, "alpha", nativeHookMetadataFPAlpha) {
				t.Fatalf("contributor refresh proof wrong: %+v", proof)
			}

			nativeBefore, nativeMode, nativeMtime = fileSnapshot(t, path)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			runNativeHookMetadataReconcile(t, key, repoRoot,
				nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, alphaOnly), false)
			assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

func TestNativeHookMetadataContributorOrderIsDeterministic(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	recordedEvidence := func(order []nativeHookMetadataContributor) ownership.FileState {
		repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
		key, wantRoot, spec, _ := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
		runNativeHookMetadataReconcile(t, key, repoRoot,
			nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, order), false)
		record, err := nativeHookMetadataStore(t, wantRoot).Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		state, ok := record.Files[spec.rel]
		if !ok {
			t.Fatalf("no recorded file state for %s", spec.rel)
		}
		return state
	}
	forward := recordedEvidence([]nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
		{resource: "beta", fingerprint: nativeHookMetadataFPBeta},
	})
	reverse := recordedEvidence([]nativeHookMetadataContributor{
		{resource: "beta", fingerprint: nativeHookMetadataFPBeta},
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("contributor input order changed the recorded evidence: %+v vs %+v", forward, reverse)
	}
}

func TestNativeHookMetadataStaleSatisfactionDropped(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
			seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, seed, 0o640); err != nil {
				t.Fatal(err)
			}
			seedNativeHookJSONLock(t, wantRoot)
			work := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
				{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
			})
			runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
			store := nativeHookMetadataStore(t, wantRoot)
			if len(nativeHookMetadataProof(t, store, spec.rel).HookSatisfactions()) != 1 {
				t.Fatal("seed reconcile recorded no satisfaction")
			}

			retaining := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
				{resource: "gamma", fingerprint: nativeHookMetadataFPGamma},
			})
			retaining.incomplete = map[string]bool{tc.rel: true}
			runNativeHookMetadataReconcile(t, key, repoRoot, retaining, false)
			records := nativeHookMetadataProof(t, store, spec.rel).HookSatisfactions()
			if len(records) != 1 {
				t.Fatalf("incomplete reconcile satisfaction count = %d", len(records))
			}
			merged := map[ownership.HookSourceEvidence]bool{}
			for _, source := range records[0].Sources {
				merged[source] = true
			}
			if !merged[ownership.HookSourceEvidence{Resource: "alpha", SourceDigest: nativeHookMetadataFPAlpha}] ||
				!merged[ownership.HookSourceEvidence{Resource: "gamma", SourceDigest: nativeHookMetadataFPGamma}] {
				t.Fatalf("incomplete reconcile did not merge retained + selected evidence: %+v", records[0].Sources)
			}

			edited := bytes.Replace(seed,
				[]byte(nativeHookJSONSelectedCommand), []byte(nativeHookJSONOtherCommand), 1)
			if bytes.Equal(edited, seed) {
				t.Fatal("edit did not change the handwritten value")
			}
			if err := os.WriteFile(path, edited, 0o640); err != nil {
				t.Fatal(err)
			}
			runNativeHookMetadataReconcile(t, key, repoRoot, retaining, false)
			if records := nativeHookMetadataProof(t, store, spec.rel).HookSatisfactions(); len(records) != 0 {
				t.Fatalf("edited native value kept stale satisfaction: %+v", records)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(nativeHookJSONOtherCommand)) ||
				!bytes.Contains(data, []byte(nativeHookJSONSelectedCommand)) ||
				bytes.Contains(data, []byte("_managed_by")) {
				t.Fatal("reconcile dropped the edited handwritten entry or added a marker")
			}
		})
	}
}

func TestNativeHookMetadataMixedOwnedAndHandwritten(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)

	work := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	otherEntries, err := ownedJSONHookEntries(tc.rel, map[string][]json.RawMessage{
		tc.otherEvent: {json.RawMessage(nativeHookMetadataRule(t, tc, "echo d9-owned-distinct"))},
	}, "owned-other")
	if err != nil {
		t.Fatal(err)
	}
	otherEntries[0].sourceDigest = nativeHookMetadataFPGamma
	work.add(otherEntries[0])

	runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
	store := nativeHookMetadataStore(t, wantRoot)
	proof := nativeHookMetadataProof(t, store, spec.rel)
	if proof.OwnedCount() != 1 || len(proof.HookSatisfactions()) != 1 {
		t.Fatalf("mixed proof = owned %v satisfactions %+v", proof.OwnedIDs(), proof.HookSatisfactions())
	}
	ownedID := proof.OwnedIDs()[0]
	if proof.HookSatisfactions()[0].ID() == ownedID {
		t.Fatal("owned and satisfaction records overlap on identity")
	}

	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
	updated := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPBeta},
	})
	updated.add(otherEntries[0])
	report := runNativeHookMetadataReconcile(t, key, repoRoot, updated, false)
	assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
	for _, root := range report.PerRoot {
		for _, written := range root.Written {
			if strings.HasSuffix(written, spec.rel) {
				t.Fatalf("source-only refresh claimed an aggregate write: %+v", root)
			}
		}
	}
	proof = nativeHookMetadataProof(t, store, spec.rel)
	records := proof.HookSatisfactions()
	if len(records) != 1 || !reflect.DeepEqual(records[0].Sources, []ownership.HookSourceEvidence{
		{Resource: "alpha", SourceDigest: nativeHookMetadataFPBeta},
	}) {
		t.Fatalf("source-only update = %+v", records)
	}
}

func TestNativeHookMetadataDryRunPreservesPendingState(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	runNativeHookMetadataReconcile(t, key, repoRoot,
		nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
			{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
		}), false)
	store := nativeHookMetadataStore(t, wantRoot)

	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	stateTreeBefore := satisfactionPathTree(t, filepath.Join(home, "state"))
	report := runNativeHookMetadataReconcile(t, key, repoRoot,
		nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
			{resource: "beta", fingerprint: nativeHookMetadataFPBeta},
		}), true)
	for _, root := range report.PerRoot {
		if len(root.Written) != 0 || len(root.Removed) != 0 {
			t.Fatalf("dry run claimed changes: %+v", root)
		}
	}
	assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	if got := satisfactionPathTree(t, filepath.Join(home, "state")); !reflect.DeepEqual(got, stateTreeBefore) {
		t.Fatalf("dry run changed the state tree: %+v -> %+v", stateTreeBefore, got)
	}
	proof := nativeHookMetadataProof(t, store, spec.rel)
	if len(proof.HookSatisfactions()) != 1 {
		t.Fatal("dry run mutated recorded satisfaction proof")
	}
}

func nativeHookMetadataOwnedEntry(t *testing.T, tc nativeHookJSONLayoutCase, event, command, resource, fingerprint string) ownedJSONEntry {
	t.Helper()
	entries, err := ownedJSONHookEntries(tc.rel, map[string][]json.RawMessage{
		event: {json.RawMessage(nativeHookMetadataRule(t, tc, command))},
	}, resource)
	if err != nil || len(entries) != 1 {
		t.Fatalf("construct owned entry: %v / %d", err, len(entries))
	}
	entries[0].sourceDigest = fingerprint
	return entries[0]
}

func nativeHookMetadataProducedState(t *testing.T, tc nativeHookJSONLayoutCase) (outputKey, string, *ownership.Store, string, string) {
	t.Helper()
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	work := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	work.add(nativeHookMetadataOwnedEntry(t, tc, tc.otherEvent, "echo d9-owned-extra", "owned-res", nativeHookMetadataFPBeta))
	runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
	store := nativeHookMetadataStore(t, wantRoot)
	proof := nativeHookMetadataProof(t, store, spec.rel)
	if proof.OwnedCount() != 1 || len(proof.HookSatisfactions()) != 1 {
		t.Fatalf("seed proof = owned %v satisfactions %+v", proof.OwnedIDs(), proof.HookSatisfactions())
	}
	return key, repoRoot, store, wantRoot, path
}

func assertNativeHookMetadataCleared(t *testing.T, store *ownership.Store, rel string) {
	t.Helper()
	proof, err := store.Proof(rel)
	if err != nil {
		t.Fatalf("Proof after cleanup: %v", err)
	}
	if proof.OwnedCount() != 0 || len(proof.HookSatisfactions()) != 0 {
		t.Fatalf("recorded hook metadata survived absent native content: owned=%v satisfactions=%+v", proof.OwnedIDs(), proof.HookSatisfactions())
	}
}

func TestNativeHookMetadataAbsentCleanup(t *testing.T) {
	t.Run("factory_events_removed_keep_unrelated", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "factory_repo")
		key, repoRoot, store, _, path := nativeHookMetadataProducedState(t, tc)
		kept := []byte("{\n  \"SessionStart\" : [\n    " + nativeHookJSONGroup("echo d9-kept-unrelated", tc.matcher, "") + "\n  ]\n}\n")
		if err := os.WriteFile(path, kept, 0o640); err != nil {
			t.Fatal(err)
		}
		before, mode, mtime := fileSnapshot(t, path)
		runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
		assertFileUnchanged(t, path, before, mode, mtime)
		assertNativeHookMetadataCleared(t, store, tc.rel)
	})
	t.Run("claude_hooks_member_removed_keeps_settings", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "claude_repo")
		key, repoRoot, store, _, path := nativeHookMetadataProducedState(t, tc)
		kept := []byte("{\n  /* preserve unrelated root settings */\n  \"otherSettings\" : { \"keep\" : \"d9-unrelated-root-setting\" }\n}\n")
		if err := os.WriteFile(path, kept, 0o640); err != nil {
			t.Fatal(err)
		}
		before, mode, mtime := fileSnapshot(t, path)
		runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
		assertFileUnchanged(t, path, before, mode, mtime)
		assertNativeHookMetadataCleared(t, store, tc.rel)
	})
	t.Run("factory_file_removed_complete", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "factory_repo")
		key, repoRoot, store, _, path := nativeHookMetadataProducedState(t, tc)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
		assertPathAbsent(t, path)
		assertNativeHookMetadataCleared(t, store, tc.rel)
	})
	t.Run("factory_file_removed_incomplete", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "factory_repo")
		key, repoRoot, store, _, path := nativeHookMetadataProducedState(t, tc)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		work := &ownedJSONAggregateWork{incomplete: map[string]bool{tc.rel: true}}
		runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
		assertPathAbsent(t, path)
		assertNativeHookMetadataCleared(t, store, tc.rel)
	})
	t.Run("factory_output_root_removed", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "factory_repo")
		key, repoRoot, store, wantRoot, path := nativeHookMetadataProducedState(t, tc)
		if err := os.RemoveAll(wantRoot); err != nil {
			t.Fatal(err)
		}
		runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
		assertPathAbsent(t, path)
		assertPathAbsent(t, wantRoot)
		assertNativeHookMetadataCleared(t, store, tc.rel)
	})
	t.Run("no_record_no_file_noop", func(t *testing.T) {
		tc := nativeHookJSONCaseByName(t, "factory_repo")
		repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
		key, wantRoot, _, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
		report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot,
			&ownedJSONAggregateWork{}, nativeHookJSONOptions(), false)
		if report == nil || len(report.Diags) != 0 {
			t.Fatalf("absent aggregate with no record produced diags: %+v", report)
		}
		assertPathAbsent(t, path)
		assertPathAbsent(t, wantRoot)
		store := nativeHookMetadataStore(t, wantRoot)
		if store.EverRecorded() {
			t.Fatal("true no-op created ownership evidence")
		}
	})
}

func TestNativeHookMetadataSharedFilePreservesMCPProof(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "gemini_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	mcpOwned := `{"command":"run-old"}`
	mcpCanonical, err := canonicalOwnedJSON([]byte(mcpOwned))
	if err != nil {
		t.Fatal(err)
	}
	mcpID := ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "owned-srv", IdentityDigest: ownership.McpIdentityDigest("owned-srv")}
	mcpEntry := ownership.Entry{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "owned-srv",
		IdentityDigest: mcpID.IdentityDigest, ValueDigest: ownership.Digest(mcpCanonical), Resource: "owned-srv-res"}
	handMcpID := ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "handwritten-srv", IdentityDigest: ownership.McpIdentityDigest("handwritten-srv")}
	seed := []byte("{\n  // keep this comment\n  \"otherSettings\" : { \"keep\" : true },\n  \"mcpServers\" : {\n    \"owned-srv\" : " + mcpOwned + ",\n    \"handwritten-srv\" : { \"command\" : \"user-cmd\" }\n  },\n  \"hooks\" : {\n    \"BeforeTool\" : [\n      {\n        /* identical handwritten matcher group */\n        \"matcher\" : " + nativeHookJSONJSONString(tc.matcher) + ",\n        \"hooks\" : [ { \"type\" : \"command\", \"command\" : " + nativeHookJSONJSONString(nativeHookJSONSelectedCommand) + " } ]\n      }\n    ]\n  }\n}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	store := nativeHookMetadataStore(t, wantRoot)
	if _, err := store.Apply(ownership.Mutation{
		Rel: spec.rel, AbsFile: path, NextBytes: seed,
		Entries: []ownership.Entry{mcpEntry}, Reason: ownership.ReasonSync,
		ExpectPriorDigest: ownership.Digest(seed),
	}); err != nil {
		t.Fatalf("seed owned MCP proof: %v", err)
	}
	assertMCPProof := func(stage string) {
		t.Helper()
		proof := nativeHookMetadataProof(t, store, spec.rel)
		entry, ok := proof.Entry(mcpID)
		if !ok {
			t.Fatalf("%s: shared file lost recorded MCP proof", stage)
		}
		if entry.Resource != mcpEntry.Resource || entry.ValueDigest != mcpEntry.ValueDigest || entry.IdentityDigest != mcpEntry.IdentityDigest {
			t.Fatalf("%s: recorded MCP entry changed: %+v", stage, entry)
		}
		if _, ok := proof.Entry(handMcpID); ok {
			t.Fatalf("%s: handwritten MCP server was adopted", stage)
		}
	}
	assertMCPProof("seeded")

	satisfied := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	runNativeHookMetadataReconcile(t, key, repoRoot, satisfied, false)
	assertMCPProof("satisfaction reconcile")
	if proof := nativeHookMetadataProof(t, store, spec.rel); len(proof.HookSatisfactions()) != 1 || proof.OwnedCount() != 1 {
		t.Fatalf("satisfaction reconcile proof = %+v", proof)
	}

	ownedWork := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	ownedWork.add(nativeHookMetadataOwnedEntry(t, tc, tc.otherEvent, "echo d9-shared-owned", "owned-hook", nativeHookMetadataFPGamma))
	runNativeHookMetadataReconcile(t, key, repoRoot, ownedWork, false)
	assertMCPProof("owned hook reconcile")
	proof := nativeHookMetadataProof(t, store, spec.rel)
	if proof.OwnedCount() != 2 || len(proof.HookSatisfactions()) != 1 {
		t.Fatalf("owned hook reconcile proof = owned %v sats %+v", proof.OwnedIDs(), proof.HookSatisfactions())
	}
	var ownedHookID ownership.EntryID
	for _, id := range proof.OwnedIDs() {
		if id.Kind == ownership.KindHook {
			ownedHookID = id
		}
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(current, []byte(`"run-old"`), []byte(`"run-new"`), 1)
	if bytes.Equal(edited, current) {
		t.Fatal("MCP edit did not change the shared file")
	}
	if err := os.WriteFile(path, edited, 0o640); err != nil {
		t.Fatal(err)
	}
	editedBytes, editedMode, editedMtime := fileSnapshot(t, path)
	sourceOnly := nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand, []nativeHookMetadataContributor{
		{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
	})
	sourceOnly.add(nativeHookMetadataOwnedEntry(t, tc, tc.otherEvent, "echo d9-shared-owned", "owned-hook", nativeHookMetadataFPDelta))
	runNativeHookMetadataReconcile(t, key, repoRoot, sourceOnly, false)
	assertFileUnchanged(t, path, editedBytes, editedMode, editedMtime)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"run-new"`)) || !bytes.Contains(data, []byte("keep this comment")) ||
		!bytes.Contains(data, []byte(`"user-cmd"`)) {
		t.Fatal("hook-only reconcile rewrote unrelated MCP or comment bytes")
	}
	assertMCPProof("source-only reconcile")
	newMcpCanonical, err := canonicalOwnedJSON([]byte(`{"command":"run-new"}`))
	if err != nil {
		t.Fatal(err)
	}
	proof = nativeHookMetadataProof(t, store, spec.rel)
	if status := proof.Classify(mcpID, ownership.Digest(newMcpCanonical)); status != ownership.StatusOwnedDrifted {
		t.Fatalf("edited MCP entry classified %s, want owned-drifted (recorded digest must stay old)", status)
	}
	if status := proof.Classify(mcpID, mcpEntry.ValueDigest); status != ownership.StatusOwnedClean {
		t.Fatalf("recorded MCP digest was refreshed to edited bytes: %s", status)
	}
	if !proof.HasHookSource(ownedHookID, "owned-hook", nativeHookMetadataFPDelta) {
		t.Fatalf("source-only update did not refresh hook source evidence: %+v", proof.OwnedIDs())
	}
	if len(proof.HookSatisfactions()) != 1 {
		t.Fatalf("satisfaction authority lost: %+v", proof.HookSatisfactions())
	}
}

func TestNativeHookMetadataProducedProofSatisfiesReader(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, _, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	secondCommand := "echo d9-second-identical"
	thirdCommand := "echo d9-third-identical"
	handwritten := func(command string) string {
		return "{\n      /* handwritten twin */\n      \"matcher\" : " + nativeHookJSONJSONString(tc.matcher) +
			",\n      \"hooks\" : [ { \"type\" : \"command\", \"command\" : " + nativeHookJSONJSONString(command) + " } ]\n    }"
	}
	seed := []byte("{\n  " + nativeHookJSONJSONString(tc.event) + " : [\n    " + handwritten(nativeHookJSONSelectedCommand) +
		"\n  ],\n  " + nativeHookJSONJSONString(tc.otherEvent) + " : [\n    " + handwritten(secondCommand) +
		"\n  ],\n  \"SessionStart\" : [\n    " + handwritten(thirdCommand) + "\n  ]\n}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	entries, err := ownedJSONHookEntries(tc.rel, map[string][]json.RawMessage{
		tc.event:       {json.RawMessage(nativeHookMetadataRule(t, tc, nativeHookJSONSelectedCommand))},
		tc.otherEvent:  {json.RawMessage(nativeHookMetadataRule(t, tc, secondCommand))},
		"SessionStart": {json.RawMessage(nativeHookMetadataRule(t, tc, thirdCommand))},
	}, "alpha")
	if err != nil || len(entries) != 3 {
		t.Fatalf("construct desired entries: %v / %d", err, len(entries))
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		entry.sourceDigest = nativeHookMetadataFPAlpha
		if entry.container == "SessionStart" {
			entry.resource = "beta"
			entry.sourceDigest = nativeHookMetadataFPBeta
		}
		work.add(entry)
	}
	runNativeHookMetadataReconcile(t, key, repoRoot, work, false)
	store := nativeHookMetadataStore(t, wantRoot)

	ins, err := inspectNativeHookFile(store, wantRoot, tc.target, tc.machineScope)
	if err != nil {
		t.Fatalf("inspectNativeHookFile: %v", err)
	}
	if len(ins.proof.OwnedIDs()) != 0 {
		t.Fatalf("satisfaction-only proof claimed ownership: %v", ins.proof.OwnedIDs())
	}
	if len(ins.proof.HookSatisfactions()) != 3 {
		t.Fatalf("produced satisfactions = %+v", ins.proof.HookSatisfactions())
	}
	if !ins.hookSatisfied(nativeHookMetadataFPAlpha, "alpha") {
		t.Fatal("two-identity produced proof did not satisfy its resource/fingerprint")
	}
	if !ins.hookSatisfied(nativeHookMetadataFPBeta, "beta") {
		t.Fatal("single-identity produced proof did not satisfy its resource/fingerprint")
	}
	if ins.hookSatisfied(nativeHookMetadataFPAlpha, "gamma") {
		t.Fatal("reader matched a satisfaction under the wrong resource")
	}
	if ins.hookSatisfied(nativeHookMetadataFPGamma, "alpha") {
		t.Fatal("reader matched a satisfaction under the wrong fingerprint")
	}

	withoutSecond := []byte("{\n  " + nativeHookJSONJSONString(tc.event) + " : [\n    " + handwritten(nativeHookJSONSelectedCommand) +
		"\n  ],\n  \"SessionStart\" : [\n    " + handwritten(thirdCommand) + "\n  ]\n}\n")
	if err := os.WriteFile(path, withoutSecond, 0o640); err != nil {
		t.Fatal(err)
	}
	ins, err = inspectNativeHookFile(store, wantRoot, tc.target, tc.machineScope)
	if err != nil {
		t.Fatalf("inspectNativeHookFile after removal: %v", err)
	}
	if ins.hookSatisfied(nativeHookMetadataFPAlpha, "alpha") {
		t.Fatal("reader accepted partial proof: one of two recorded identities was removed")
	}
	if !ins.hookSatisfied(nativeHookMetadataFPBeta, "beta") {
		t.Fatal("intact satisfaction was lost by a sibling identity removal")
	}

	edited := bytes.Replace(seed,
		[]byte(nativeHookJSONSelectedCommand), []byte(nativeHookJSONOtherCommand), 1)
	if bytes.Equal(edited, seed) {
		t.Fatal("edit did not change the handwritten value")
	}
	if err := os.WriteFile(path, edited, 0o640); err != nil {
		t.Fatal(err)
	}
	ins, err = inspectNativeHookFile(store, wantRoot, tc.target, tc.machineScope)
	if err != nil {
		t.Fatalf("inspectNativeHookFile after drift: %v", err)
	}
	if ins.hookSatisfied(nativeHookMetadataFPAlpha, "alpha") {
		t.Fatal("reader accepted partial proof: one of two recorded identities drifted")
	}
	if !ins.hookSatisfied(nativeHookMetadataFPBeta, "beta") {
		t.Fatal("intact satisfaction was lost by a sibling identity drift")
	}
}

func nativeHookMetadataPlanFor(t *testing.T, store *ownership.Store, spec ownedJSONFileSpec, path string, satisfactions []ownership.HookSatisfaction) []dependencytxn.DesiredFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read aggregate for plan: %v", err)
	}
	proof, err := store.Proof(spec.rel)
	if err != nil {
		t.Fatalf("Proof for plan: %v", err)
	}
	plan := ownedJSONPatch{satisfactions: satisfactions}
	desired, err := nativeOwnedJSONTxnPlan(store, spec.rel, path, raw, nil, raw, false, plan, proof)
	if err != nil {
		t.Fatalf("nativeOwnedJSONTxnPlan: %v", err)
	}
	if len(desired) != 2 || desired[0].Path != path || desired[1].Path != store.StateFile() {
		t.Fatalf("prepared plan = %+v, want aggregate assertion + state file", desired)
	}
	return desired
}

func nativeHookMetadataSatisfaction(t *testing.T, tc nativeHookJSONLayoutCase, command string, pairs ...ownership.HookSourceEvidence) ownership.HookSatisfaction {
	t.Helper()
	valueDigest := ownership.Digest([]byte(nativeHookMetadataRule(t, tc, command)))
	return ownership.HookSatisfaction{
		Container:      tc.event,
		IdentityDigest: ownership.HookIdentityDigest(tc.event, valueDigest),
		ValueDigest:    valueDigest,
		Sources:        pairs,
	}
}

func TestNativeHookMetadataAggregateCASRefusesRace(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	runNativeHookMetadataReconcile(t, key, repoRoot, nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand,
		[]nativeHookMetadataContributor{
			{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
		}), false)
	store := nativeHookMetadataStore(t, wantRoot)

	control := nativeHookMetadataPlanFor(t, store, spec, path, []ownership.HookSatisfaction{
		nativeHookMetadataSatisfaction(t, tc, nativeHookJSONSelectedCommand,
			ownership.HookSourceEvidence{Resource: "alpha", SourceDigest: nativeHookMetadataFPAlpha},
			ownership.HookSourceEvidence{Resource: "beta", SourceDigest: nativeHookMetadataFPBeta}),
	})
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
	if _, err := commitNativeHookFiles(context.Background(), control); err != nil {
		t.Fatalf("control commit: %v", err)
	}
	assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)

	desired := nativeHookMetadataPlanFor(t, store, spec, path, []ownership.HookSatisfaction{
		nativeHookMetadataSatisfaction(t, tc, nativeHookJSONSelectedCommand,
			ownership.HookSourceEvidence{Resource: "gamma", SourceDigest: nativeHookMetadataFPGamma}),
	})
	edited := bytes.Replace(seed,
		[]byte(nativeHookJSONSelectedCommand), []byte("echo raced"), 1)
	if bytes.Equal(edited, seed) {
		t.Fatal("race edit did not change the aggregate")
	}
	if err := os.WriteFile(path, edited, 0o640); err != nil {
		t.Fatal(err)
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	racedBytes, racedMode, racedMtime := fileSnapshot(t, path)
	_, err := commitNativeHookFiles(context.Background(), desired)
	if err == nil || !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("commit after aggregate race = %v, want ErrGenerationConflict", err)
	}
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	assertFileUnchanged(t, path, racedBytes, racedMode, racedMtime)
	stateDir, err := dependencytxn.StateDir()
	if err == nil {
		if err := dependencytxn.CheckPending(stateDir); err != nil {
			t.Fatalf("failed commit left a pending journal: %v", err)
		}
	}
}

func TestNativeHookMetadataPendingTransactionRecovers(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	runNativeHookMetadataReconcile(t, key, repoRoot, nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand,
		[]nativeHookMetadataContributor{
			{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
		}), false)
	store := nativeHookMetadataStore(t, wantRoot)

	desired := nativeHookMetadataPlanFor(t, store, spec, path, []ownership.HookSatisfaction{
		nativeHookMetadataSatisfaction(t, tc, nativeHookJSONSelectedCommand,
			ownership.HookSourceEvidence{Resource: "gamma", SourceDigest: nativeHookMetadataFPGamma}),
	})
	crash := errors.New("injected crash")
	_, err := commitNativeHookFiles(context.Background(), desired, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultAfterPublish {
			return crash
		}
		return nil
	}))
	if !errors.Is(err, crash) {
		t.Fatalf("faulted commit = %v, want injected crash", err)
	}
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot,
		&ownedJSONAggregateWork{}, nativeHookJSONOptions(), true)
	found := false
	for _, d := range report.Diags {
		if strings.Contains(d.Message, "pending transaction") {
			found = true
		}
	}
	if !found || !diag.HasCode(report.Diags, nativeHookJSONOptions().writeCode) {
		t.Fatalf("read-only reconcile did not report the pending transaction: %+v", report.Diags)
	}
	assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)

	runNativeHookMetadataReconcile(t, key, repoRoot, &ownedJSONAggregateWork{}, false)
	records := nativeHookMetadataProof(t, store, spec.rel).HookSatisfactions()
	if len(records) != 0 {
		t.Fatalf("recovered run kept satisfaction from deselected contributors: %+v", records)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("journal still pending after mutating reconcile: %v", err)
	}
}

func TestNativeHookMetadataFirstUsePendingJournalRecovers(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	store := nativeHookMetadataStore(t, wantRoot)
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}

	proof, _ := store.Proof(spec.rel)
	entry := nativeHookMetadataOwnedEntry(t, tc, tc.event, nativeHookJSONSelectedCommand, "alpha", nativeHookMetadataFPAlpha)
	next := []byte("{\n  " + nativeHookJSONJSONString(tc.event) + " : [\n    " +
		nativeHookJSONGroup(nativeHookJSONSelectedCommand, tc.matcher, "") + "\n  ]\n}\n")
	desired, err := nativeOwnedJSONTxnPlan(store, spec.rel, path, nil, os.ErrNotExist, next, true,
		ownedJSONPatch{owned: []ownership.Entry{entry.ownershipEntry()}}, proof)
	if err != nil {
		t.Fatalf("nativeOwnedJSONTxnPlan: %v", err)
	}
	if len(desired) != 2 || desired[0].Path != path || desired[1].Path != store.StateFile() {
		t.Fatalf("first-use plan = %+v, want aggregate + state file", desired)
	}
	for _, f := range desired {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
			t.Fatalf("prepare desired parent: %v", err)
		}
	}
	crash := errors.New("injected crash")
	_, err = commitNativeHookFiles(context.Background(), desired, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultAfterJournal {
			return crash
		}
		return nil
	}))
	if !errors.Is(err, crash) {
		t.Fatalf("faulted first-use commit = %v, want injected crash", err)
	}
	assertPathAbsent(t, path)
	assertPathAbsent(t, store.StateFile())
	if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("first-use crash left no pending journal: %v", err)
	}

	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot,
		&ownedJSONAggregateWork{}, nativeHookJSONOptions(), true)
	found := false
	for _, d := range report.Diags {
		if strings.Contains(d.Message, "pending transaction") {
			found = true
		}
	}
	if !found || !diag.HasCode(report.Diags, nativeHookJSONOptions().writeCode) {
		t.Fatalf("read-only reconcile did not report the pending first-use transaction: %+v", report.Diags)
	}
	assertPathAbsent(t, path)
	assertPathAbsent(t, store.StateFile())
	if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("read-only reconcile cleared the pending journal: %v", err)
	}

	report = reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot,
		&ownedJSONAggregateWork{}, nativeHookJSONOptions(), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("recovering reconcile failed: %+v", report)
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("journal still pending after mutating reconcile: %v", err)
	}
	assertNativeHookMetadataCleared(t, store, tc.rel)
	if data, err := os.ReadFile(path); err == nil &&
		bytes.Contains(data, []byte(nativeHookJSONSelectedCommand)) {
		t.Fatal("recovered aggregate still contains the deselected hook")
	}
}

func TestNativeHookMetadataCommitRechecksAssertionAfterStaging(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	key, wantRoot, spec, path := nativeHookMetadataTarget(t, tc, repoRoot, home, codexHome)
	seed, _, _, _ := nativeHookJSONIdenticalSeed(tc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	seedNativeHookJSONLock(t, wantRoot)
	runNativeHookMetadataReconcile(t, key, repoRoot, nativeHookMetadataWork(t, tc, nativeHookJSONSelectedCommand,
		[]nativeHookMetadataContributor{
			{resource: "alpha", fingerprint: nativeHookMetadataFPAlpha},
		}), false)
	store := nativeHookMetadataStore(t, wantRoot)

	desired := nativeHookMetadataPlanFor(t, store, spec, path, []ownership.HookSatisfaction{
		nativeHookMetadataSatisfaction(t, tc, nativeHookJSONSelectedCommand,
			ownership.HookSourceEvidence{Resource: "gamma", SourceDigest: nativeHookMetadataFPGamma}),
	})
	edited := bytes.Replace(seed,
		[]byte(nativeHookJSONSelectedCommand), []byte("echo raced-after-stage"), 1)
	if bytes.Equal(edited, seed) {
		t.Fatal("race edit did not change the aggregate")
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	fired := false
	_, err := commitNativeHookFiles(context.Background(), desired, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultAfterStage && !fired {
			fired = true
			return os.WriteFile(path, edited, 0o640)
		}
		return nil
	}))
	if !fired {
		t.Fatal("FaultAfterStage hook never ran")
	}
	if err == nil || !errors.Is(err, dependencytxn.ErrRecoveryBlocked) || errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("post-staging assertion recheck = %v, want ErrRecoveryBlocked", err)
	}
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	racedBytes, racedMode, racedMtime := fileSnapshot(t, path)
	if !bytes.Equal(racedBytes, edited) {
		t.Fatal("raced aggregate bytes differ from the independently authored edit")
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("post-staging fault left no pending journal: %v", err)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); !errors.Is(err, dependencytxn.ErrRecoveryBlocked) {
		t.Fatalf("recovery over foreign aggregate bytes = %v, want ErrRecoveryBlocked", err)
	}
	assertFileUnchanged(t, path, racedBytes, racedMode, racedMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("blocked recovery cleared the pending journal: %v", err)
	}
}
