package materialize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativeHookBackup struct {
	rel string
	abs string
}

type nativeHookAggregateSnapshot struct {
	abs        string
	rel        string
	raw        []byte
	generation dependencytxn.Generation
	mode       fs.FileMode
}

type nativeHookSupportAuthority struct {
	cleanSources map[string]bool
	assertions   []dependencytxn.DesiredFile
}

type nativeHookSupportInstaller func(context.Context, bool, nativeHookSupportAuthority) (bool, error)

type nativeHookTransactionExtras struct {
	originalNative        *nativeHookAggregateSnapshot
	nextBytes             []byte
	nextSet               bool
	plan                  ownedJSONPatch
	planSet               bool
	participants          []dependencytxn.DesiredFile
	assertions            []dependencytxn.DesiredFile
	backups               []nativeHookBackup
	commitOptions         []dependencytxn.Option
	beforeNativeAssertion func() error
	diagnostics           []diag.Diagnostic
	hookChanges           []HookChange
	pending               bool
	refused               bool
}

type nativeHookFinalizer func(context.Context, *ownership.Store, outputKey, string, ownedJSONFileSpec, string, []byte, error, *jsonc.Document, ownedJSONPatch, ownership.Proof, error, bool, bool) (nativeHookTransactionExtras, error)

func nativeHookAggregateSnapshotAtPlan(abs, rel string, raw []byte, readErr error) (*nativeHookAggregateSnapshot, error) {
	live, generation, mode, err := dependencytxn.ReadSnapshot(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
	}
	if errors.Is(readErr, os.ErrNotExist) {
		if generation.Exists {
			return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
		}
		return &nativeHookAggregateSnapshot{abs: abs, rel: rel, generation: generation, mode: mode}, nil
	}
	if readErr != nil {
		return nil, readErr
	}
	if !generation.Exists || !bytes.Equal(live, raw) {
		return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
	}
	return &nativeHookAggregateSnapshot{
		abs: abs, rel: rel, raw: append([]byte(nil), raw...), generation: generation, mode: mode,
	}, nil
}

func nativeHookAggregateSnapshotMatches(snapshot *nativeHookAggregateSnapshot, live []byte, generation dependencytxn.Generation, mode fs.FileMode) bool {
	if snapshot == nil {
		return true
	}
	if generation != snapshot.generation {
		return false
	}
	if !snapshot.generation.Exists {
		return true
	}
	return bytes.Equal(live, snapshot.raw) && mode.Perm() == snapshot.mode.Perm()
}

func revalidateNativeHookAggregateSnapshot(snapshot *nativeHookAggregateSnapshot) error {
	if snapshot == nil {
		return nil
	}
	live, generation, mode, err := dependencytxn.ReadSnapshot(snapshot.abs)
	if err != nil {
		return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, snapshot.abs)
	}
	if !nativeHookAggregateSnapshotMatches(snapshot, live, generation, mode) {
		return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, snapshot.abs)
	}
	return nil
}

func nativeHookCleanSupportSources(file ownedJSONFileSpec, raw []byte, proof ownership.Proof, proofErr error) (map[string]bool, error) {
	clean := make(map[string]bool)
	if proofErr != nil || len(raw) == 0 {
		return clean, nil
	}
	doc, err := jsonc.Parse(file.rel, raw)
	if err != nil {
		return clean, fmt.Errorf("parse current native hook file: %w", err)
	}
	container, _, err := ownedJSONContainerNode(doc, file)
	if err != nil || container == nil {
		return clean, err
	}
	existing, err := ownedJSONExistingEntries(file, container)
	if err != nil {
		return nil, err
	}
	current := make(map[ownership.EntryID]string, len(existing))
	duplicates := make(map[ownership.EntryID]bool)
	for _, entry := range existing {
		id := entry.value.ID
		if _, ok := current[id]; ok {
			duplicates[id] = true
		}
		current[id] = entry.value.ValueDigest
	}
	ids := proof.OwnedIDs()
	sources := make(map[string]ownership.HookSourceEvidence)
	for _, id := range ids {
		if id.Kind != ownership.KindHook {
			continue
		}
		entry, ok := proof.Entry(id)
		if !ok {
			continue
		}
		if entry.Resource != "" && entry.SourceDigest != "" {
			pair := ownership.HookSourceEvidence{Resource: entry.Resource, SourceDigest: entry.SourceDigest}
			sources[hookSupportOwnerID(pair.Resource, pair.SourceDigest)] = pair
		}
		for _, pair := range entry.SourceEvidence {
			if pair.Resource != "" && pair.SourceDigest != "" {
				sources[hookSupportOwnerID(pair.Resource, pair.SourceDigest)] = pair
			}
		}
	}
	for key, pair := range sources {
		matches := 0
		complete := true
		for _, id := range ids {
			if !proof.HasHookSource(id, pair.Resource, pair.SourceDigest) {
				continue
			}
			matches++
			valueDigest, ok := current[id]
			if !ok || duplicates[id] || proof.Classify(id, valueDigest) != ownership.StatusOwnedClean {
				complete = false
			}
		}
		if matches > 0 && complete {
			clean[key] = true
		}
	}
	return clean, nil
}

func nativeHookSupportPreflightCleanSources(key outputKey, repoRoot string) (map[string]bool, error) {
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nil, err
	}
	raw, err := ownership.ReadAggregateFileAt(destination.Root, file.rel)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		return nil, err
	}
	proof, proofErr := store.Proof(file.rel)
	return nativeHookCleanSupportSources(file, raw, proof, proofErr)
}

func nativeHookSupportProofsEqual(left, right ownership.Proof) bool {
	if left.File() != right.File() || left.Generation() != right.Generation() || left.ContentDigest() != right.ContentDigest() {
		return false
	}
	leftIDs := left.OwnedIDs()
	rightIDs := right.OwnedIDs()
	if !reflect.DeepEqual(leftIDs, rightIDs) {
		return false
	}
	for _, id := range leftIDs {
		leftEntry, leftOK := left.Entry(id)
		rightEntry, rightOK := right.Entry(id)
		if !leftOK || !rightOK || !reflect.DeepEqual(leftEntry, rightEntry) {
			return false
		}
	}
	return reflect.DeepEqual(left.HookSatisfactions(), right.HookSatisfactions())
}

func nativeHookSupportProofStateAssertion(store *ownership.Store, rel string, planningProof ownership.Proof) (dependencytxn.DesiredFile, error) {
	if store == nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: native hook ownership store is unavailable", dependencytxn.ErrGenerationConflict)
	}
	statePath := store.StateFile()
	beforeBytes, beforeGeneration, beforeMode, err := dependencytxn.ReadSnapshot(statePath)
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: snapshot native hook ownership state: %v", dependencytxn.ErrGenerationConflict, err)
	}
	freshProof, err := store.Proof(rel)
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: reload native hook ownership proof: %v", dependencytxn.ErrGenerationConflict, err)
	}
	afterBytes, afterGeneration, afterMode, err := dependencytxn.ReadSnapshot(statePath)
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: recheck native hook ownership state: %v", dependencytxn.ErrGenerationConflict, err)
	}
	if !beforeGeneration.Exists || beforeGeneration != afterGeneration || beforeMode != afterMode ||
		!bytes.Equal(beforeBytes, afterBytes) || !nativeHookSupportProofsEqual(planningProof, freshProof) {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: native hook ownership proof changed before support publication", dependencytxn.ErrGenerationConflict)
	}
	return dependencytxn.DesiredFile{
		Path: statePath, Expected: afterGeneration, ExpectedMode: afterMode,
		Bytes: afterBytes, Mode: afterMode,
	}, nil
}

func nativeHookSupportSnapshotAssertion(snapshot *nativeHookAggregateSnapshot) dependencytxn.DesiredFile {
	assertion := dependencytxn.DesiredFile{Path: snapshot.abs, Expected: snapshot.generation, ExpectedMode: snapshot.mode}
	if snapshot.generation.Exists {
		assertion.Bytes = append([]byte(nil), snapshot.raw...)
		assertion.Mode = snapshot.mode
	} else {
		assertion.Delete = true
	}
	return assertion
}

func nativeHookActivationFinalizer(key outputKey, repoRoot string, mcpEntries []ownedJSONEntry, mcpIncomplete bool, markMCPIncomplete func(), mcpDiagnostics *[]diag.Diagnostic, supportInstallers ...nativeHookSupportInstaller) nativeHookFinalizer {
	return func(ctx context.Context, store *ownership.Store, _ outputKey, outDir string, file ownedJSONFileSpec, nativeAbs string, nativeRaw []byte, nativeReadErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras := nativeHookTransactionExtras{}
		nativeSnapshot, snapshotErr := nativeHookAggregateSnapshotAtPlan(nativeAbs, file.rel, nativeRaw, nativeReadErr)
		if snapshotErr != nil {
			extras.refused = true
			if errors.Is(snapshotErr, dependencytxn.ErrGenerationConflict) {
				return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: file.rel}, snapshotErr)
			}
			return extras, snapshotErr
		}
		extras.originalNative = nativeSnapshot
		var supportInstall nativeHookSupportInstaller
		if len(supportInstallers) > 0 {
			supportInstall = supportInstallers[0]
		}
		if nativeHookSharesMCPRoute(key, repoRoot) {
			if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
				return nativeHookTransactionExtras{refused: true, diagnostics: []diag.Diagnostic{{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: "shared native ownership state is unavailable; aggregate was left unchanged"}}}, nil
			}
			mcpDoc, cloneErr := jsonc.Parse(file.rel, doc.Bytes())
			var mcpPlan ownedJSONPatch
			var mcpDiags []diag.Diagnostic
			mcpErr := cloneErr
			if mcpErr == nil {
				mcpPlan, mcpDiags, mcpErr = planGeminiMCPContainer(mcpDoc, key, outDir, file, mcpEntries, proof, proofErr, mcpIncomplete)
			}
			if mcpErr != nil {
				if markMCPIncomplete != nil {
					markMCPIncomplete()
				}
				diagnostic := nativeHookMCPDiagnostic(key, mcpErr)
				extras.diagnostics = append(extras.diagnostics, diagnostic)
				if mcpDiagnostics != nil {
					*mcpDiagnostics = append(*mcpDiagnostics, diagnostic)
				}
				extras.plan = plan
				extras.planSet = true
			} else {
				extras.diagnostics = append(extras.diagnostics, mcpDiags...)
				extras.nextBytes = mcpDoc.Bytes()
				extras.nextSet = true
				extras.plan = combineOwnedJSONPatches(plan, mcpPlan)
				extras.planSet = true
			}
		}
		declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
		if err != nil {
			extras.refused = true
			extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
			return extras, nil
		}
		expectedEvents := make(map[string]bool)
		for _, entry := range plan.owned {
			if entry.Kind == ownership.KindHook {
				expectedEvents[entry.Container] = true
			}
		}
		for _, entry := range plan.satisfied {
			if entry.kind == ownership.KindHook {
				expectedEvents[entry.container] = true
			}
		}
		var legacySnapshots []*nativeHookAggregateSnapshot
		for _, declaredLegacy := range declaration.file.Legacy {
			legacyRel := path.Join(declaration.partition.Prefix, declaredLegacy)
			legacyAbs := filepath.Join(outDir, filepath.FromSlash(legacyRel))
			legacyParent := path.Dir(legacyRel)
			if legacyParent != "." {
				if _, err := validatePortableOutputDir(outDir, legacyParent); err != nil {
					extras.refused = true
					extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
					return extras, nil
				}
			}
			// The legacy file could not be surveyed safely for a
			// surviving managed twin: fail this target closed so the
			// merge can neither rewrite nor delete those bytes.
			legacyRaw, readErr := ownership.ReadAggregateFileAt(outDir, legacyRel)
			if errors.Is(readErr, os.ErrNotExist) {
				snapshot, snapshotErr := nativeHookAggregateSnapshotAtPlan(legacyAbs, legacyRel, nil, readErr)
				if snapshotErr != nil {
					extras.refused = true
					return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: legacyRel}, snapshotErr)
				}
				legacySnapshots = append(legacySnapshots, snapshot)
				extras.assertions = append(extras.assertions, dependencytxn.DesiredFile{Path: legacyAbs, Expected: snapshot.generation, Delete: true})
				continue
			}
			if readErr != nil {
				extras.refused = true
				extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
				return extras, nil
			}
			legacyDoc, parseErr := jsonc.Parse(legacyRel, legacyRaw)
			if parseErr != nil {
				extras.refused = true
				extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
				return extras, nil
			}
			next, changed, deleteFile, removals, patchErr := nativeHookLegacyPatch(legacyDoc, declaration.file.Member, expectedEvents)
			if patchErr != nil {
				extras.refused = true
				extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
				return extras, nil
			}
			snapshot, snapshotErr := nativeHookAggregateSnapshotAtPlan(legacyAbs, legacyRel, legacyRaw, nil)
			if snapshotErr != nil {
				extras.refused = true
				return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: legacyRel}, snapshotErr)
			}
			legacySnapshots = append(legacySnapshots, snapshot)
			if !incomplete && changed {
				participant := dependencytxn.DesiredFile{Path: legacyAbs, Expected: snapshot.generation, ExpectedMode: snapshot.mode}
				if deleteFile {
					participant.Delete = true
				} else {
					participant.Bytes = next
					participant.Mode = snapshot.mode
				}
				extras.participants = append(extras.participants, participant)
				extras.backups = append(extras.backups, nativeHookBackup{rel: legacyRel, abs: legacyAbs})
				for _, removal := range removals {
					extras.hookChanges = append(extras.hookChanges, HookChange{
						Target:       key.target,
						MachineScope: key.machineScope,
						File:         legacyAbs,
						Event:        removal.event,
						Matcher:      removal.matcher,
						Before:       removal.before,
					})
				}
				extras.pending = true
			} else {
				extras.assertions = append(extras.assertions, dependencytxn.DesiredFile{
					Path: legacyAbs, Expected: snapshot.generation, ExpectedMode: snapshot.mode,
					Bytes: append([]byte(nil), snapshot.raw...), Mode: snapshot.mode,
				})
			}
		}
		if supportInstall != nil {
			authority := nativeHookSupportAuthority{cleanSources: map[string]bool{}}
			if !key.machineScope {
				var sourceErr error
				authority.cleanSources, sourceErr = nativeHookCleanSupportSources(file, nativeSnapshot.raw, proof, proofErr)
				if sourceErr != nil {
					extras.refused = true
					return extras, diag.NewCodedError(diag.CodeMaterializeHookSupportConflict, diag.SourceRef{File: file.rel}, sourceErr)
				}
				if len(authority.cleanSources) > 0 {
					proofAssertion, assertionErr := nativeHookSupportProofStateAssertion(store, file.rel, proof)
					if assertionErr != nil {
						extras.refused = true
						return extras, diag.NewCodedError(diag.CodeMaterializeHookSupportConflict, diag.SourceRef{File: file.rel}, assertionErr)
					}
					authority.assertions = append(authority.assertions, nativeHookSupportSnapshotAssertion(nativeSnapshot))
					for _, snapshot := range legacySnapshots {
						authority.assertions = append(authority.assertions, nativeHookSupportSnapshotAssertion(snapshot))
					}
					authority.assertions = append(authority.assertions, proofAssertion)
				}
			}
			pending, installErr := supportInstall(ctx, dryRun, authority)
			if installErr != nil {
				extras.refused = true
				if _, coded := diag.AsCodedError(installErr); coded {
					return extras, installErr
				}
				extras.diagnostics = append(extras.diagnostics, diag.Diagnostic{
					Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target),
					Message: "Hook support could not be installed safely; Native publication and legacy cleanup were refused",
				})
				return extras, nil
			}
			extras.pending = extras.pending || pending
			if snapshotErr := revalidateNativeHookAggregateSnapshot(nativeSnapshot); snapshotErr != nil {
				extras.refused = true
				return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: file.rel}, snapshotErr)
			}
			for _, snapshot := range legacySnapshots {
				if snapshotErr := revalidateNativeHookAggregateSnapshot(snapshot); snapshotErr != nil {
					extras.refused = true
					return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: snapshot.rel}, snapshotErr)
				}
			}
		}
		if !incomplete {
			supportExtras, supportErr := nativeHookSupportCleanup(key, repoRoot, outDir, declaration)
			if supportErr != nil {
				extras.refused = true
				if _, coded := diag.AsCodedError(supportErr); coded {
					return extras, supportErr
				}
				if errors.Is(supportErr, dependencytxn.ErrGenerationConflict) {
					return extras, diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: outDir}, supportErr)
				}
				extras.diagnostics = append(extras.diagnostics, nativeHookLegacyRefusal(key))
				return extras, nil
			}
			extras.participants = append(extras.participants, supportExtras.participants...)
			extras.assertions = append(extras.assertions, supportExtras.assertions...)
			extras.backups = append(extras.backups, supportExtras.backups...)
			extras.pending = extras.pending || supportExtras.pending
			extras.diagnostics = append(extras.diagnostics, supportExtras.diagnostics...)
		}
		return extras, nil
	}
}

func nativeHookMCPDiagnostic(key outputKey, err error) diag.Diagnostic {
	if coded, ok := diag.AsCodedError(err); ok {
		diagnostic := coded.Diagnostic("mcpServers")
		diagnostic.Target = string(key.target)
		return diagnostic
	}
	return diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Resource: "mcpServers", Message: "shared MCP container could not be reconciled safely; previous entries were retained"}
}

func combineOwnedJSONPatches(hooks, mcps ownedJSONPatch) ownedJSONPatch {
	hooks.owned = append(hooks.owned, mcps.owned...)
	hooks.collisions = append(hooks.collisions, mcps.collisions...)
	hooks.drifted = append(hooks.drifted, mcps.drifted...)
	hooks.prune = append(hooks.prune, mcps.prune...)
	hooks.prunePostponed = append(hooks.prunePostponed, mcps.prunePostponed...)
	hooks.satisfied = append(hooks.satisfied, mcps.satisfied...)
	hooks.satisfactions = append(hooks.satisfactions, mcps.satisfactions...)
	for key, raw := range mcps.writeMcp {
		hooks.writeMcp[key] = raw
	}
	hooks.deleteMcp = append(hooks.deleteMcp, mcps.deleteMcp...)
	hooks.pruneSuppressed = hooks.pruneSuppressed || mcps.pruneSuppressed
	if len(mcps.reconciledKinds) > 0 {
		if hooks.reconciledKinds == nil {
			hooks.reconciledKinds = make(map[ownership.EntryKind]bool)
		}
		for kind, reconciled := range mcps.reconciledKinds {
			hooks.reconciledKinds[kind] = reconciled
		}
	}
	return hooks
}

func nativeHookLegacyRefusal(key outputKey) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeMaterializeHookLegacyMalformed,
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Message:  "a declared legacy hook aggregate could not be reconciled safely; native and legacy files were left unchanged",
	}
}

func nativeHookLegacySupportDirs(key outputKey, declaration nativeHookAggregateDeclaration) []string {
	info, ok := harness.TargetInfoFor(key.target)
	if !ok {
		return nil
	}
	preserved := info.PreservedLegacyDirs.Project
	if key.machineScope {
		preserved = info.PreservedLegacyDirs.User
	}
	seen := make(map[string]bool)
	var dirs []string
	for _, legacy := range declaration.file.Legacy {
		parent := path.Clean(path.Dir(path.Join(declaration.partition.Prefix, legacy)))
		if parent == "." {
			continue
		}
		var narrower, exact []string
		for _, candidate := range preserved {
			candidate = path.Clean(candidate)
			if candidate == parent {
				exact = append(exact, candidate)
			} else if slashPathWithin(parent, candidate) {
				narrower = append(narrower, candidate)
			}
		}
		selected := narrower
		if len(selected) == 0 {
			selected = exact
		}
		if len(selected) == 0 && parent != "." {
			selected = []string{parent}
		}
		for _, dir := range selected {
			if info.PreservesLegacyPath(dir, key.machineScope) {
				continue
			}
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

func slashPathWithin(parent, child string) bool {
	parent = path.Clean(parent)
	child = path.Clean(child)
	if parent == "." {
		return child != "." && !path.IsAbs(child)
	}
	return strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}

func nativeHookPortableProtectedPaths(key outputKey, repoRoot, outDir string) map[string]bool {
	protected := make(map[string]bool)
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return protected
	}
	root, rootErr := ownership.CanonicalRoot(outDir)
	if rootErr != nil {
		return protected
	}
	destinationRoot, destinationErr := ownership.CanonicalRoot(destination.Root)
	if destinationErr != nil {
		return protected
	}
	add := func(rel string, directory bool) {
		if rel == "" || rel == "." {
			return
		}
		abs := filepath.Join(destinationRoot, filepath.FromSlash(rel))
		pathRel, err := filepath.Rel(root, abs)
		if err != nil || pathRel == "." || pathRel == ".." || strings.HasPrefix(pathRel, ".."+string(filepath.Separator)) || filepath.IsAbs(pathRel) {
			return
		}
		protectedRel := filepath.ToSlash(pathRel)
		if directory {
			protectedRel += "/"
		}
		protected[protectedRel] = true
	}
	add(file.rel, false)
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if err != nil {
		return protected
	}
	for _, legacy := range declaration.file.Legacy {
		add(path.Join(declaration.partition.Prefix, legacy), false)
	}
	for _, supportDir := range nativeHookLegacySupportDirs(key, declaration) {
		add(supportDir, true)
	}
	return protected
}

func nativeHookExcludedSupportFiles(key outputKey) map[string]bool {
	excluded := make(map[string]bool)
	if layout, ok := targetmeta.LookupNativeLayout(targetmeta.Target(key.target)); ok {
		for _, partition := range layout.Partitions {
			for _, aggregate := range partition.Aggregates {
				file := aggregate.Project
				if key.machineScope {
					file = aggregate.User
				}
				if file.Path != "" {
					excluded[path.Join(partition.Prefix, file.Path)] = true
				}
				for _, legacy := range file.Legacy {
					excluded[path.Join(partition.Prefix, legacy)] = true
				}
			}
		}
	}
	if info, ok := harness.TargetInfoFor(key.target); ok {
		for _, rel := range info.AggregateFilesForScope(key.machineScope) {
			excluded[path.Clean(rel)] = true
		}
	}
	return excluded
}

type nativeHookLegacyRemoval struct {
	event   string
	matcher string
	before  json.RawMessage
}

func nativeHookLegacyPatch(doc *jsonc.Document, member string, expectedEvents map[string]bool) ([]byte, bool, bool, []nativeHookLegacyRemoval, error) {
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return nil, false, false, nil, fmt.Errorf("legacy hook root is not an object")
	}
	container := root
	if member != "" {
		var ok bool
		container, ok = root.Member(member)
		if !ok {
			return doc.Bytes(), false, false, nil, nil
		}
		if container.Kind() != jsonc.KindObject {
			return nil, false, false, nil, fmt.Errorf("legacy hook envelope is not an object")
		}
	}
	changed := false
	var removals []nativeHookLegacyRemoval
	for _, event := range container.Keys() {
		eventNode, ok := container.Member(event)
		if !ok {
			continue
		}
		if eventNode.Kind() != jsonc.KindArray {
			if member == "" && !expectedEvents[event] {
				continue
			}
			return nil, false, false, nil, fmt.Errorf("legacy hook event is not an array")
		}
		var elements [][]byte
		eventChanged := false
		for i := 0; i < eventNode.Len(); i++ {
			entry, _ := eventNode.Element(i)
			if entry.Kind() != jsonc.KindObject {
				return nil, false, false, nil, fmt.Errorf("legacy hook entry is not an object")
			}
			if isManagedNode(entry) {
				canonical, err := canonicalOwnedJSON(entry.Raw())
				if err != nil {
					return nil, false, false, nil, fmt.Errorf("legacy hook entry cannot be canonicalized")
				}
				removals = append(removals, nativeHookLegacyRemoval{event: event, matcher: hookReviewMatcher(canonical), before: canonical})
				eventChanged = true
				changed = true
				continue
			}
			if handlers, hasHandlers := entry.Member("hooks"); hasHandlers {
				if handlers.Kind() != jsonc.KindArray {
					return nil, false, false, nil, fmt.Errorf("legacy hook group handlers are not an array")
				}
				var retained [][]byte
				var removed []json.RawMessage
				handlersChanged := false
				for j := 0; j < handlers.Len(); j++ {
					handler, _ := handlers.Element(j)
					if handler.Kind() != jsonc.KindObject {
						return nil, false, false, nil, fmt.Errorf("legacy hook handler is not an object")
					}
					if isManagedNode(handler) {
						canonical, err := canonicalOwnedJSON(handler.Raw())
						if err != nil {
							return nil, false, false, nil, fmt.Errorf("legacy hook handler cannot be canonicalized")
						}
						removed = append(removed, canonical)
						handlersChanged = true
						continue
					}
					retained = append(retained, handler.Raw())
				}
				if handlersChanged {
					removedRaw, err := json.Marshal(removed)
					if err != nil {
						return nil, false, false, nil, fmt.Errorf("legacy hook removal cannot be encoded")
					}
					parts := map[string]json.RawMessage{"hooks": removedRaw}
					if matcherNode, hasMatcher := entry.Member("matcher"); hasMatcher {
						parts["matcher"] = matcherNode.Raw()
					}
					groupRaw, err := json.Marshal(parts)
					if err != nil {
						return nil, false, false, nil, fmt.Errorf("legacy hook removal cannot be encoded")
					}
					canonical, err := canonicalOwnedJSON(groupRaw)
					if err != nil {
						return nil, false, false, nil, fmt.Errorf("legacy hook removal cannot be canonicalized")
					}
					removals = append(removals, nativeHookLegacyRemoval{event: event, matcher: hookReviewMatcher(canonical), before: canonical})
					doc.SetElements(handlers, retained)
					eventChanged = true
					changed = true
				}
			}
			elements = append(elements, entry.Raw())
		}
		if eventChanged {
			if len(elements) == 0 {
				doc.DeleteMember(container, event)
			} else {
				doc.SetElements(eventNode, elements)
			}
		}
	}
	if changed && member != "" && len(container.Keys()) == 0 {
		doc.DeleteMember(root, member)
	}
	if changed && len(root.Keys()) == 0 {
		return nil, true, true, removals, nil
	}
	return doc.Bytes(), doc.Changed(), false, removals, nil
}

func nativeHookPortableManifestPath(repoRoot string, machineScope bool) (string, error) {
	manifestPath, err := portableManifestPath(repoRoot, machineScope)
	if err != nil {
		return repoRoot, err
	}
	root := repoRoot
	if machineScope {
		root, err = os.UserHomeDir()
		if err != nil {
			return manifestPath, err
		}
	}
	rel, err := filepath.Rel(root, manifestPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return manifestPath, fmt.Errorf("portable manifest path is outside its authorized root")
	}
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent != "." {
		if _, err := validatePortableOutputDir(root, parent); err != nil {
			return manifestPath, err
		}
	}
	info, err := os.Lstat(manifestPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return manifestPath, fmt.Errorf("portable manifest is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return manifestPath, err
	}
	return manifestPath, nil
}

func nativeHookSupportCleanup(key outputKey, repoRoot, outDir string, declaration nativeHookAggregateDeclaration) (nativeHookTransactionExtras, error) {
	extras := nativeHookTransactionExtras{}
	supportDirs := nativeHookLegacySupportDirs(key, declaration)
	if len(supportDirs) == 0 {
		return extras, nil
	}
	manifestPath, err := nativeHookPortableManifestPath(repoRoot, key.machineScope)
	if err != nil {
		return extras, portableManifestError(manifestPath, err)
	}
	state, stateErr := loadPortableState(repoRoot, key.machineScope)
	if stateErr != nil {
		return extras, portableManifestError(manifestPath, stateErr)
	}
	manifestBytes, manifestGeneration, manifestMode, snapshotErr := dependencytxn.ReadSnapshot(state.path)
	if snapshotErr != nil {
		return extras, portableManifestError(state.path, snapshotErr)
	}
	if err := validatePortableManifestSnapshot(state, manifestBytes, manifestGeneration, manifestMode); err != nil {
		return extras, portableManifestError(state.path, err)
	}
	excluded := nativeHookExcludedSupportFiles(key)
	var removals []dependencytxn.DesiredFile
	removedManifest := false
	for _, supportDir := range supportDirs {
		dirAbs, err := validatePortableOutputDir(outDir, supportDir)
		if err != nil {
			return nativeHookTransactionExtras{}, err
		}
		dirInfo, err := os.Lstat(dirAbs)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nativeHookTransactionExtras{}, err
		}
		if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
			return nativeHookTransactionExtras{}, fmt.Errorf("legacy support component is not a real directory")
		}
		walkErr := filepath.WalkDir(dirAbs, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if candidate == dirAbs || entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(outDir, candidate)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if excluded[rel] {
				return nil
			}
			info, err := os.Lstat(candidate)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, generation, err := dependencytxn.ReadSymlinkSnapshot(candidate)
				if err != nil {
					return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, candidate)
				}
				if !nativeHookCacheLinkContained(candidate, linkTarget) {
					return nil
				}
				removals = append(removals, dependencytxn.DesiredFile{
					Path: candidate, Expected: generation, ExpectedLinkTarget: linkTarget, Delete: true,
				})
				if manifestEntry, ok := state.entries[portableManifestKey(key.target, rel)]; ok && manifestEntry.Digest != "" &&
					nativeHookSymlinkSnapshotDigest(info.Mode(), linkTarget) == manifestEntry.Digest {
					state.remove(key.target, rel)
					removedManifest = true
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			manifestEntry, ok := state.entries[portableManifestKey(key.target, rel)]
			if !ok || manifestEntry.Digest == "" {
				return nil
			}
			data, generation, mode, err := dependencytxn.ReadSnapshot(candidate)
			if err != nil {
				return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, candidate)
			}
			if nativeHookRegularSnapshotDigest(data, mode) != manifestEntry.Digest {
				return nil
			}
			removals = append(removals, dependencytxn.DesiredFile{
				Path: candidate, Expected: generation, ExpectedMode: mode, Delete: true,
			})
			state.remove(key.target, rel)
			removedManifest = true
			return nil
		})
		if walkErr != nil {
			return nativeHookTransactionExtras{}, walkErr
		}
	}
	if len(removals) == 0 {
		return extras, nil
	}
	extras.participants = append(extras.participants, removals...)
	extras.pending = true
	if removedManifest {
		manifestNext, hasEntries, err := state.serialized()
		if err != nil {
			return nativeHookTransactionExtras{}, err
		}
		if hasEntries && !bytes.Equal(manifestBytes, manifestNext) {
			extras.participants = append(extras.participants, dependencytxn.DesiredFile{
				Path: state.path, Expected: manifestGeneration, ExpectedMode: manifestMode,
				Bytes: manifestNext, Mode: manifestMode,
			})
		} else if !hasEntries && manifestGeneration.Exists {
			extras.participants = append(extras.participants, dependencytxn.DesiredFile{
				Path: state.path, Expected: manifestGeneration, ExpectedMode: manifestMode, Delete: true,
			})
		} else {
			extras.assertions = append(extras.assertions, portableManifestAssertion(state.path, manifestBytes, manifestGeneration, manifestMode))
		}
	} else {
		extras.assertions = append(extras.assertions, portableManifestAssertion(state.path, manifestBytes, manifestGeneration, manifestMode))
	}
	return extras, nil
}

func validatePortableManifestSnapshot(state *portableState, data []byte, generation dependencytxn.Generation, mode fs.FileMode) error {
	if state.exists != generation.Exists || !bytes.Equal(state.raw, data) || (state.exists && state.mode.Perm() != mode.Perm()) {
		return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, state.path)
	}
	return nil
}

func portableManifestError(path string, err error) error {
	code := "materialize.manifest.error"
	if errors.Is(err, dependencytxn.ErrGenerationConflict) {
		code = diag.CodeOwnershipGenerationConflict
	}
	return diag.NewCodedError(code, diag.SourceRef{File: path}, err)
}

func portableManifestAssertion(path string, data []byte, generation dependencytxn.Generation, mode fs.FileMode) dependencytxn.DesiredFile {
	assertion := dependencytxn.DesiredFile{Path: path, Expected: generation}
	if generation.Exists {
		assertion.ExpectedMode = mode
		assertion.Bytes = data
		assertion.Mode = mode
	} else {
		assertion.Delete = true
	}
	return assertion
}

func nativeHookCacheLinkContained(link, target string) bool {
	cacheRoot, err := filepath.Abs(cache.CacheDir())
	if err != nil {
		return false
	}
	cacheRoot, err = filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		return false
	}
	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(filepath.Dir(link), targetPath)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return false
	}
	inside, err := portablePathWithinRoot(cacheRoot, resolvedTarget)
	if err != nil || !inside {
		return false
	}
	info, err := os.Stat(resolvedTarget)
	return err == nil && info.Mode().IsRegular()
}

func nativeHookRegularSnapshotDigest(data []byte, mode fs.FileMode) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "file\x00\x00%#o\x00%d\x00", mode.Perm(), len(data))
	_, _ = h.Write(data)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func nativeHookSymlinkSnapshotDigest(mode fs.FileMode, target string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "symlink\x00\x00%#o\x00%d\x00%s", mode.Perm(), len(target), target)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func nativeHookPruneFencePaths(target hcc.Target, machineScope bool) []string {
	key := outputKey{target: target, machineScope: machineScope}
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(target), machineScope)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var paths []string
	add := func(rel string, directory bool) {
		rel = path.Clean(rel)
		if rel == "" || rel == "." {
			return
		}
		if directory {
			rel += "/"
		}
		if seen[rel] {
			return
		}
		seen[rel] = true
		paths = append(paths, rel)
	}
	add(path.Join(declaration.partition.Prefix, declaration.file.Path), false)
	for _, legacy := range declaration.file.Legacy {
		add(path.Join(declaration.partition.Prefix, legacy), false)
	}
	for _, dir := range nativeHookLegacySupportDirs(key, declaration) {
		add(dir, true)
	}
	sort.Strings(paths)
	return paths
}
