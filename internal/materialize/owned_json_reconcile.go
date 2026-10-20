package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// ownedJSONHookIdentity returns the total identity digest for a hook rule. The
// identity is derived from the hook event and the digest of the canonical JSON
// rule, so any difference in any byte of the canonical value or in the event
// produces a different identity. The canonical rule is the same bytes that are
// written for the entry and that become the ownership ValueDigest, so identity
// equality implies value equality.
//
// The rule must already be canonical JSON (sorted keys, no insignificant
// whitespace). Both ownedJSONHookEntries and ownedJSONExistingEntries canonicalize
// before calling this function.
func ownedJSONHookIdentity(event string, rule []byte) (string, error) {
	return ownership.HookIdentityDigest(event, ownership.Digest(rule)), nil
}

// ownedJSONAggregateReport is the per-file outcome, folded into the run Report.
type ownedJSONAggregateReport struct {
	written     bool
	diags       []diag.Diagnostic
	hookChanges []HookChange
}

// reconcileOwnedJSONAggregates reconciles every aggregate file for one output
// key. Each file is independent: a malformed document blocks only that file.
//
// # The critical section, and why it spans the whole of this function
//
// Everything from before store.Recover to after the last iteration of the rels
// loop runs inside ONE [ownership.Store.WithWriteSessionContext]. The boundary is
// forced, not chosen, and three separate facts pick it:
//
//   - The caller's read must be inside it. reconcileOwnedJSONAggregateFile reads the
//     aggregate bytes and then store.Proof, computes a patch from both, and only
//     then calls store.Apply. Apply's own acquisition cannot serialize a read its
//     caller already performed, so two processes could both read, then commit one
//     after the other, and the second patch — computed from bytes the first had
//     already replaced — silently DROPPED the first one's entry. That lost update
//     is the residual defect this section closes; it reproduced 60/60.
//   - Recover must be inside it. Recover is a WRITER: it rolls an incomplete
//     generation forward, rolls it back, or abandons it. Outside the section,
//     process B's Recover can abandon process A's genuinely in-flight generation.
//   - The loop must be inside it AS A WHOLE, not per file. The journal is one slot
//     per canonical root and Record.Generation is one counter covering all of the
//     root's files, so releasing between files would let another process interleave
//     a generation into the same counter — and would also let a contended root burn
//     the whole lock timeout once per file
//     before reporting anything. One outer section collapses that to one wait.
//
// The inner Apply, Recover, and BackupOnly acquisitions become reentrant depth
// bumps on this Store rather than second descriptors. They are SEQUENTIAL, which
// is what keeps them clear of [ownership.ErrNestedMutation]: that guard sits inside
// withWriteLock and refuses a mutation that starts while another is half written,
// not a widened session that contains several one after another.
//
// # Dry run takes no lock at all
//
// `sync --check`, `status`, `list`, the TUI background refresh and `doctor`'s
// preview all reach this function only through WithDryRun. Dry run already skips
// Recover, BackupOnly and Apply, so it must also skip the session: acquiring would
// create locks/<rootKey>.lock under the state root — breaking Open's documented
// side-effect-free contract — and would make a read-only command block behind a
// long-running sync.
func reconcileOwnedJSONAggregates(ctx context.Context, report *Report, key outputKey, outDir string, files []ownedJSONFileSpec, work *ownedJSONAggregateWork, options ownedJSONReconcileOptions, dryRun bool) *Report {
	files = append([]ownedJSONFileSpec(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	desiredByRel := make(map[string][]ownedJSONEntry, len(files))
	if work != nil {
		for _, entry := range work.entries {
			desiredByRel[entry.rel] = append(desiredByRel[entry.rel], entry)
		}
	}
	// Nothing desired and no file present means there is no aggregate work at
	// all; do not open a store or create empty files for it.
	anyWork := len(desiredByRel) > 0
	hasNative := false
	for _, file := range files {
		if file.hookShape != "" {
			hasNative = true
			if options.nativeHookFinalize != nil {
				anyWork = true
			}
		}
		if _, err := os.Lstat(filepath.Join(outDir, file.rel)); err == nil {
			anyWork = true
		}
	}
	needsRoot := anyWork
	if !anyWork && !hasNative && options.batch == nil {
		return report
	}

	// The observer is how a WAIT reaches the report. A contended acquisition that
	// eventually succeeds returns no error, so without it the user is never told
	// why their sync paused; and a degradation is a property of the root that must
	// be reported once, not once per file.
	lockReport := options.lockReport
	if lockReport == nil {
		lockReport = newOwnedAggregateLockReporter(key, outDir)
	}
	store := options.store
	var err error
	if store == nil {
		store, err = ownership.Open(outDir, ownership.WithLockObserver(lockReport.observe))
		if err != nil {
			report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "",
				fmt.Sprintf("open ownership store for %s: %v", outDir, err)))
			return report
		}
	} else {
		canonical, err := ownership.CanonicalRoot(outDir)
		if err != nil {
			report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "",
				fmt.Sprintf("resolve aggregate root %s: %v", outDir, err)))
			return report
		}
		if store.RootKey() != ownership.RootKey(canonical) {
			report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "",
				fmt.Sprintf("shared ownership store does not match aggregate root %s", outDir)))
			return report
		}
	}
	if options.batch != nil {
		if !dryRun && hasNative {
			if depErr := recoverNativeDependencyTxn(ctx); depErr != nil {
				report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", depErr, options.writeCode))
				return report
			}
		}
		if _, err := options.batch.root(ctx, store, dryRun); err != nil {
			report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", err, options.writeCode))
			return report
		}
		for _, outcome := range options.batch.unreportedRecoveries(store) {
			report.Diags = append(report.Diags, ownedAggregateDiag(diag.CodeOwnershipRecovered, key, outcome.File,
				fmt.Sprintf("recovered an incomplete ownership generation %d for %q (phase %s, action %s)",
					outcome.Generation, ownedAggregateLabel(outDir, outcome.File), outcome.Phase, outcome.Action)))
		}
		options.storeSessionHeld = !dryRun
		options.store = store
	}

	if !anyWork {
		for _, file := range files {
			if file.hookShape == "" && options.batch == nil {
				continue
			}
			var proof ownership.Proof
			var proofErr error
			if options.batch != nil {
				proof, proofErr = options.batch.proof(ctx, store, file.rel, dryRun)
				if proofErr == nil {
					if len(options.pluginInstanceKeys) > 0 {
						proof, _, _ = proof.ForPluginInstances(options.pluginInstanceKeys, options.activePluginInstances)
					} else {
						proof, _, _ = proof.ForPluginInstance(options.pluginInstanceKey, options.activePluginInstances)
					}
				}
			} else {
				proof, proofErr = store.Proof(file.rel)
			}
			if proofErr != nil {
				continue
			}
			if proof.OwnedCount() > 0 || len(proof.HookSatisfactions()) > 0 {
				anyWork = true
			}
		}
		if !anyWork && hasNative {
			stateDir, depErr := dependencytxn.StateDir()
			if depErr == nil {
				depErr = dependencytxn.CheckPending(stateDir)
			}
			switch {
			case errors.Is(depErr, dependencytxn.ErrRecoveryRequired):
				anyWork = true
			case depErr != nil:
				report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", depErr, options.writeCode))
				return report
			}
		}
		if !anyWork {
			return report
		}
	}
	if !dryRun && needsRoot {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "", fmt.Sprintf("create aggregate root %s: %v", outDir, err)))
			return report
		}
	}

	// Dry run: no session, no lock, no state-root writes. See the doc comment.
	if dryRun {
		if hasNative {
			stateDir, depErr := dependencytxn.StateDir()
			if depErr == nil {
				depErr = dependencytxn.CheckPending(stateDir)
			}
			if depErr != nil {
				report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", depErr, options.writeCode))
			}
		}
		return appendOwnedJSONAggregateReports(ctx, report, store, key, outDir, files, desiredByRel, work, options, true)
	}
	if options.storeSessionHeld {
		if hasNative && options.batch == nil {
			if depErr := recoverNativeDependencyTxn(ctx); depErr != nil {
				report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", depErr, options.writeCode))
				return report
			}
		}
		return appendOwnedJSONAggregateReports(ctx, report, store, key, outDir, files, desiredByRel, work, options, false)
	}

	sessionErr := store.WithWriteSessionContext(ctx, options.sessionName, func() error {
		// "On startup, incomplete generations are replayed or rolled back
		// according to the recorded phase" — once, before any mutation, and
		// INSIDE the section so exactly one process ever resolves a journal.
		outcomes, recoverErr := store.Recover()
		for _, outcome := range outcomes {
			report.Diags = append(report.Diags, ownedAggregateDiag(diag.CodeOwnershipRecovered, key, outcome.File,
				fmt.Sprintf("recovered an incomplete ownership generation %d for %q (phase %s, action %s)",
					outcome.Generation, ownedAggregateLabel(outDir, outcome.File), outcome.Phase, outcome.Action)))
		}
		if recoverErr != nil {
			report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", recoverErr, options.writeCode))
			return nil
		}
		if hasNative {
			if depErr := recoverNativeDependencyTxn(ctx); depErr != nil {
				report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, outDir, "", depErr, options.writeCode))
				return nil
			}
		}
		report = appendOwnedJSONAggregateReports(ctx, report, store, key, outDir, files, desiredByRel, work, options, false)
		return nil
	})
	// The body never returns an error — every per-file failure is already a
	// diagnostic — so anything here is an ACQUISITION failure and the whole root
	// was skipped without a byte written. Report it against this root and let
	// every other root continue.
	if sessionErr != nil {
		report.Diags = append(report.Diags, ownedAggregateLockSessionDiag(key, outDir, sessionErr, options.writeCode))
		return report
	}
	if !options.deferLockDiagnostics {
		report.Diags = append(report.Diags, lockReport.diags()...)
	}
	return report
}

// appendOwnedJSONAggregateReports runs the per-file loop and folds each file's outcome
// into the report. It is a function rather than an inline loop only so the dry-run
// path can reuse it verbatim without a session around it.
func appendOwnedJSONAggregateReports(ctx context.Context, report *Report, store *ownership.Store, key outputKey, outDir string, files []ownedJSONFileSpec,
	desiredByRel map[string][]ownedJSONEntry, work *ownedJSONAggregateWork, options ownedJSONReconcileOptions, dryRun bool) *Report {
	for _, file := range files {
		fileReport := reconcileOwnedJSONAggregateFile(ctx, store, key, outDir, file, desiredByRel[file.rel], work != nil && work.incomplete[file.rel], options, dryRun)
		report.Diags = append(report.Diags, fileReport.diags...)
		report.HookChanges = append(report.HookChanges, fileReport.hookChanges...)
		if !fileReport.written {
			continue
		}
		if file.hookShape != "" {
			if len(fileReport.hookChanges) > 0 {
				if options.onWrite != nil {
					options.onWrite(report, key, file.rel, dryRun)
				} else {
					report.Diags = append(report.Diags, nativeHookReviewDiagnostic(key, file.rel, dryRun))
				}
			}
		} else if options.onWrite != nil {
			options.onWrite(report, key, file.rel, dryRun)
		}
		if options.batch != nil && !dryRun {
			options.batch.addAggregateReport(key, options.rootID, outDir, file.rel, false)
		} else {
			recordOwnedAggregateWrite(report, key, options.rootID, outDir, file.rel)
		}
	}
	return report
}

func nativeHookProofUnavailableDiagnostic(key outputKey, rel string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeHookNativeToleranceRefused,
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Source:   diag.SourceRef{File: rel},
		Message:  "Native Hook ownership evidence is unavailable; Native and legacy locations were left unchanged",
	}
}

func recoverNativeDependencyTxn(ctx context.Context) error {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(stateDir); err == nil {
		return dependencytxn.Recover(ctx, stateDir)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func reconcileOwnedJSONAggregateFile(ctx context.Context, store *ownership.Store, key outputKey, outDir string, file ownedJSONFileSpec, desired []ownedJSONEntry, incomplete bool, options ownedJSONReconcileOptions, dryRun bool) ownedJSONAggregateReport {
	var out ownedJSONAggregateReport
	rel := file.rel
	abs := filepath.Join(outDir, rel)

	var raw []byte
	var readErr error
	if options.batch != nil {
		snapshot, err := options.batch.readAggregate(ctx, store, rel, dryRun)
		if err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		raw, readErr = snapshot.bytes, snapshot.readErr
	} else {
		raw, readErr = store.ReadAggregate(rel)
	}
	switch {
	case readErr == nil:
	case os.IsNotExist(readErr):
		if len(desired) == 0 {
			if file.hookShape == "" {
				return out
			}
			if options.nativeHookFinalize == nil {
				return reconcileAbsentNativeHookMetadata(ctx, out, store, key, outDir, file, abs, options, dryRun)
			}
		}
		raw = []byte(ownedJSONEmptyDocument)
	default:
		out.diags = append(out.diags, ownedAggregateDiag(options.writeCode, key, rel,
			fmt.Sprintf("read aggregate %q: %v", ownedAggregateLabel(outDir, rel), readErr)))
		return out
	}

	doc, parseErr := jsonc.Parse(rel, raw)
	if parseErr != nil {
		// "Malformed JSON is safety-backed up before parsing, left unchanged, and
		// reported with a precise parse location."
		//
		// The backup is taken HERE rather than literally before jsonc.Parse
		// because parsing cannot mutate the file: the bytes backed up are
		// byte-identical either way, and the malformed branch is the only one
		// that needs a backup Store.Apply will not take. Backing up
		// unconditionally instead would fsync two files on every no-op sync and,
		// worse, churn the bounded five-backup retention window until the
		// genuinely valuable pre-first-merge backup was evicted.
		if readErr == nil && !dryRun {
			if _, err := store.BackupOnly(rel, abs, ownership.ReasonSync); err != nil {
				// The coded error's OWN code wins over ownership.safety-backup,
				// exactly as the Recover and Apply sites do it. Hardcoding the
				// backup code here mislabelled every acquisition failure on this
				// path — a lock timeout was reported as "the mandatory raw-byte
				// safety backup could not be written", which points a user at
				// state-directory permissions for a problem that is another
				// process holding the lock.
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel,
					fmt.Errorf("safety backup for %q failed: %w", ownedAggregateLabel(outDir, rel), err),
					diag.CodeOwnershipSafetyBackup))
			}
		}
		out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, parseErr, options.malformedCode))
		return out
	}
	if envelopeErr := validateOwnedJSONEnvelope(doc, file, os.IsNotExist(readErr)); envelopeErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, envelopeErr))
		return out
	}
	if desiredShapeErr := validateOwnedJSONDesiredHooks(file, desired); desiredShapeErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, desiredShapeErr))
		return out
	}

	container, containerMember, shapeErr := ownedJSONContainerNode(doc, file)
	if shapeErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, shapeErr))
		return out
	}

	var proof ownership.Proof
	var proofErr error
	proofReady := false
	proofReported := false
	if container == nil && len(desired) == 0 {
		if file.hookShape == "" {
			// The container does not exist and nothing is desired, so there is
			// neither anything to write nor anything that could be stale.
			return out
		}
		if options.batch != nil {
			proof, proofErr = options.batch.proof(ctx, store, rel, dryRun)
		} else {
			proof, proofErr = store.Proof(rel)
		}
		proofReady = true
		out.diags = append(out.diags, ownedAggregateProofDiags(key, outDir, rel, proofErr)...)
		proofReported = true
		if proof.OwnedCount() == 0 && len(proof.HookSatisfactions()) == 0 && options.nativeHookFinalize == nil {
			return out
		}
	}
	if container != nil && !proofReady {
		if options.batch != nil {
			proof, proofErr = options.batch.proof(ctx, store, rel, dryRun)
		} else {
			proof, proofErr = store.Proof(rel)
		}
		proofReady = true
	}

	var existing []ownedJSONExistingEntry
	var legacyRemovals []nativeHookLegacyRemoval
	if container != nil {
		if file.legacyMarkedFlatHooks && !incomplete {
			if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
				out.diags = append(out.diags, ownedAggregateProofDiags(key, outDir, rel, proofErr)...)
				out.diags = append(out.diags, nativeHookProofUnavailableDiagnostic(key, rel))
				return out
			}
			var expectedCommands map[string]map[string]bool
			if file.hookShape == targetmeta.HookShapeMatcherGroups {
				var expectedErr error
				expectedCommands, expectedErr = nativeHookExpectedCommands(rel, desired)
				if expectedErr != nil {
					out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, expectedErr))
					return out
				}
			}
			legacyRemovals, shapeErr = nativeHookLegacyMarkedFlatPatch(doc, file, expectedCommands)
			if shapeErr != nil {
				out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, shapeErr))
				return out
			}
		}
		var legacyProofs []ownership.Proof
		if options.allowOwnedLegacyFlatHooks && proofErr == nil {
			legacyProofs = append(legacyProofs, proof)
		}
		existing, shapeErr = ownedJSONExistingEntries(file, container, legacyProofs...)
		if shapeErr != nil {
			out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, shapeErr))
			return out
		}
	}
	var fallbackSnapshots []nativeHookAggregateSnapshot
	if len(options.legacyHandwrittenFiles) > 0 {
		imported, snapshots, importErr := importLegacyHandwrittenFallbacks(outDir, file, options.legacyHandwrittenFiles, existing, len(desired) > 0)
		if importErr != nil {
			var fallbackErr legacyFallbackError
			switch {
			case errors.As(importErr, &fallbackErr):
				refusal := nativeHookLegacyRefusal(key)
				refusal.Source = diag.SourceRef{File: fallbackErr.rel}
				out.diags = append(out.diags, refusal)
			case errors.Is(importErr, dependencytxn.ErrGenerationConflict):
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, importErr, options.writeCode))
			default:
				out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, importErr))
			}
			return out
		}
		existing = append(existing, imported...)
		fallbackSnapshots = snapshots
	}

	if !proofReady {
		if options.batch != nil {
			proof, proofErr = options.batch.proof(ctx, store, rel, dryRun)
		} else {
			proof, proofErr = store.Proof(rel)
		}
	}
	if !proofReported {
		out.diags = append(out.diags, ownedAggregateProofDiags(key, outDir, rel, proofErr)...)
	}
	if file.hookShape != "" && proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
		out.diags = append(out.diags, nativeHookProofUnavailableDiagnostic(key, rel))
		return out
	}
	var retainedPluginEntries []ownership.Entry
	var retainedPluginSatisfactions []ownership.HookSatisfaction
	if len(options.pluginInstanceKeys) > 0 {
		proof, retainedPluginEntries, retainedPluginSatisfactions = proof.ForPluginInstances(options.pluginInstanceKeys, options.activePluginInstances)
	} else {
		proof, retainedPluginEntries, retainedPluginSatisfactions = proof.ForPluginInstance(options.pluginInstanceKey, options.activePluginInstances)
	}

	// Advisory: the zero Proof already classifies everything as unmanaged, so
	// reconciliation continues safely (preserve, refuse, never prune) whether or
	// not the situation is worth reporting.
	desiredValues := make([]ownership.EntryValue, 0, len(desired))
	for _, entry := range desired {
		desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
	}
	existingValues := make([]ownership.EntryValue, 0, len(existing))
	for _, entry := range existing {
		existingValues = append(existingValues, entry.value)
	}
	drift := ownership.Classify(proof, existingValues, desiredValues)

	plan := ownedJSONPatchPlan(outDir, file, desired, existing, drift, proof, incomplete, options.selectedHookResources)
	out.diags = append(out.diags, plan.diags(key, options)...)

	if applyErr := applyOwnedJSONPatch(doc, container, containerMember, file, plan); applyErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, applyErr))
		return out
	}
	next := doc.Bytes()
	changed := doc.Changed()
	var extras nativeHookTransactionExtras
	if file.hookShape != "" && options.nativeHookFinalize != nil {
		var finalizeErr error
		extras, finalizeErr = options.nativeHookFinalize(ctx, store, key, outDir, file, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		out.diags = append(out.diags, extras.diagnostics...)
		if finalizeErr != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, finalizeErr, options.writeCode))
			return out
		}
		if extras.refused {
			return out
		}
		if extras.planSet {
			plan = extras.plan
		}
		if extras.nextSet {
			if !bytes.Equal(next, extras.nextBytes) {
				changed = true
			}
			next = extras.nextBytes
		} else {
			next = doc.Bytes()
		}
	}
	plan.owned = append(plan.owned, retainedPluginEntries...)
	plan.satisfactions = append(plan.satisfactions, retainedPluginSatisfactions...)
	if file.hookShape != "" {
		if appendErr := appendLegacyFallbackAssertions(&extras, fallbackSnapshots); appendErr != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, appendErr, options.writeCode))
			return out
		}
		var changes []HookChange
		if changed {
			changes = nativeHookReviewDelta(key, file, abs, existing, proof, plan)
		}
		for _, removal := range legacyRemovals {
			changes = append(changes, HookChange{
				Target:       key.target,
				MachineScope: key.machineScope,
				File:         abs,
				Event:        removal.event,
				Matcher:      removal.matcher,
				Before:       removal.before,
			})
		}
		changes = append(changes, extras.hookChanges...)
		return finishNativeOwnedJSONFile(ctx, out, store, key, outDir, file, abs, raw, readErr, next, changed, plan, proof, options, dryRun, extras, changes)
	}
	if options.batch != nil {
		snapshot, err := options.batch.readAggregate(ctx, store, rel, dryRun)
		if err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		nextBytes := doc.Bytes()
		priorGen, priorDigest := ownedAggregatePriorForApply(raw, readErr, proof)
		mutation := ownership.Mutation{
			Rel: rel, AbsFile: abs, NextBytes: nextBytes, Entries: plan.owned, Reason: ownership.ReasonSync,
			ExpectPriorGeneration: priorGen, ExpectPriorDigest: priorDigest,
		}
		if doc.Changed() {
			if err := ensureOwnedJSONHookParent(outDir, rel); err != nil {
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
				return out
			}
			var backupErr error
			if options.batch != nil {
				backupErr = options.batch.backupPreparedMutation(ctx, store, mutation, raw, readErr)
			} else {
				_, backupErr = store.BackupPreparedMutation(mutation)
			}
			if backupErr != nil {
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, backupErr, diag.CodeOwnershipSafetyBackup))
				return out
			}
		}
		if err := options.batch.addMutation(ctx, store, mutation, plan.satisfactions); err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		mode := snapshot.mode
		if !snapshot.generation.Exists {
			mode = 0o600
		}
		if err := options.batch.addParticipant(dependencytxn.DesiredFile{
			Path: abs, Expected: snapshot.generation, ExpectedMode: snapshot.mode,
			Bytes: nextBytes, Mode: mode,
		}); err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		out.written = doc.Changed()
		return out
	}
	if !doc.Changed() {
		slog.Debug("owned JSON aggregate already in sync", "target", key.target, "machine_scope", key.machineScope,
			"file", rel, "owned_entries", len(plan.owned))
		return out
	}
	if dryRun {
		out.written = true
		return out
	}

	priorGen, priorDigest := ownedAggregatePriorForApply(raw, readErr, proof)
	result, applyErr := store.Apply(ownership.Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		NextBytes:             next,
		Entries:               plan.owned,
		Reason:                ownership.ReasonSync,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, applyErr, options.writeCode))
		return out
	}
	slog.Info("owned JSON aggregate reconciled", "target", key.target, "machine_scope", key.machineScope,
		"file", rel, "generation", result.Generation, "owned_entries", len(plan.owned),
		"pruned", len(plan.prune), "collisions", len(plan.collisions), "replaced_drifted", len(plan.drifted))
	out.written = true
	return out
}

func finishNativeOwnedJSONFile(ctx context.Context, out ownedJSONAggregateReport, store *ownership.Store, key outputKey, outDir string, file ownedJSONFileSpec, abs string, raw []byte, readErr error, next []byte, changed bool, plan ownedJSONPatch, proof ownership.Proof, options ownedJSONReconcileOptions, dryRun bool, extras nativeHookTransactionExtras, changes []HookChange) ownedJSONAggregateReport {
	rel := file.rel
	if dryRun {
		out.written = changed || extras.pending
		out.hookChanges = changes
		return out
	}
	var desired []dependencytxn.DesiredFile
	if !changed && errors.Is(readErr, os.ErrNotExist) {
		if len(plan.owned) > 0 || len(plan.satisfactions) > 0 || proof.OwnedCount() > 0 || len(proof.HookSatisfactions()) > 0 {
			mutation := ownership.Mutation{
				Rel:                   rel,
				AbsFile:               abs,
				Entries:               append(append([]ownership.Entry(nil), plan.owned...), nativeRetainedNonHookEntries(plan.owned, proof, plan.reconciledKinds)...),
				Reason:                ownership.ReasonSync,
				ExpectPriorGeneration: proof.Generation(),
			}
			if options.batch != nil {
				if err := options.batch.addMutation(ctx, store, mutation, plan.satisfactions); err != nil {
					out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
					return out
				}
			} else {
				stateFile, err := store.Prepare(mutation, &plan.satisfactions)
				if err != nil {
					out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
					return out
				}
				if stateFile.Path != "" {
					desired = append(desired, stateFile)
				}
			}
		}
		if len(desired) > 0 || extras.pending {
			desired = append(desired, dependencytxn.DesiredFile{Path: abs, Expected: dependencytxn.Generation{}, Delete: true})
		}
	} else {
		var err error
		desired, err = nativeOwnedJSONTxnPlanWithBatch(ctx, store, rel, abs, raw, readErr, next, changed, plan, proof, options.batch, extras.originalNative)
		if err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
	}
	if len(desired) == 0 && !extras.pending {
		slog.Debug("native hook aggregate already in sync", "target", key.target, "machine_scope", key.machineScope,
			"file", rel, "owned_entries", len(plan.owned), "satisfactions", len(plan.satisfactions))
		return out
	}
	if extras.pending && !hasDependencyParticipant(desired, abs) {
		if extras.beforeNativeAssertion != nil {
			if err := extras.beforeNativeAssertion(); err != nil {
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
				return out
			}
		}
		assertion, err := unchangedNativeAggregateAssertion(abs, raw, readErr, extras.originalNative)
		if err != nil {
			if errors.Is(err, dependencytxn.ErrGenerationConflict) {
				err = diag.NewCodedError(diag.CodeOwnershipGenerationConflict, diag.SourceRef{File: rel}, err)
			}
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		desired = append(desired, assertion)
	}
	if changed {
		if err := ensureOwnedJSONHookParent(outDir, rel); err != nil {
			out.diags = append(out.diags, ownedAggregateDiag(options.writeCode, key, rel,
				fmt.Sprintf("prepare aggregate parent for %q: %v", ownedAggregateLabel(outDir, rel), err)))
			return out
		}
		mutation := nativeOwnedJSONMutation(rel, abs, raw, readErr, next, plan, proof)
		var backupErr error
		if options.batch != nil {
			backupErr = options.batch.backupPreparedMutation(ctx, store, mutation, raw, readErr)
		} else {
			_, backupErr = store.BackupPreparedMutation(mutation)
		}
		if backupErr != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel,
				fmt.Errorf("safety backup for %q failed: %w", ownedAggregateLabel(outDir, rel), backupErr),
				diag.CodeOwnershipSafetyBackup))
			return out
		}
	}
	for _, backup := range extras.backups {
		if changed && backup.rel == rel && backup.abs == abs {
			continue
		}
		if _, err := store.BackupOnly(backup.rel, backup.abs, ownership.ReasonSync); err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, backup.rel,
				fmt.Errorf("safety backup for %q failed: %w", ownedAggregateLabel(outDir, backup.rel), err),
				diag.CodeOwnershipSafetyBackup))
			return out
		}
	}
	if len(desired) > 0 || extras.pending {
		desired = append(desired, extras.participants...)
		desired = append(desired, extras.assertions...)
	}
	if len(desired) == 0 {
		return out
	}
	if options.batch != nil {
		for _, participant := range desired {
			if err := options.batch.addParticipant(participant); err != nil {
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
				return out
			}
		}
		out.written = changed || extras.pending
		out.hookChanges = changes
		return out
	}
	for _, participant := range desired {
		if participant.Path == abs || participant.Delete || participant.Namespace != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(participant.Path), 0o700); err != nil {
			out.diags = append(out.diags, ownedAggregateDiag(options.writeCode, key, rel,
				fmt.Sprintf("prepare transaction directory for %q: %v", ownedAggregateLabel(outDir, rel), err)))
			return out
		}
	}
	if options.batch != nil {
		for _, participant := range desired {
			if err := options.batch.addParticipant(participant); err != nil {
				out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
				return out
			}
		}
		for _, manifestCommit := range extras.manifestCommits {
			options.batch.addManifestCommit(manifestCommit)
		}
		out.written = changed || extras.pending
		out.hookChanges = changes
		return out
	}
	if _, err := commitNativeHookFiles(ctx, desired, extras.commitOptions...); err != nil {
		out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
		return out
	}
	for _, manifestCommit := range extras.manifestCommits {
		if err := refreshNativeHookManifestCommit(manifestCommit); err != nil {
			coded := portableManifestError(manifestCommit.state.path, err)
			if code, ok := diag.AsCodedError(coded); ok {
				diagnostic := code.Diagnostic(rel)
				diagnostic.Target = string(key.target)
				out.diags = append(out.diags, diagnostic)
			} else {
				out.diags = append(out.diags, diag.Diagnostic{Code: diag.CodeOwnershipGenerationConflict, Severity: diag.SeverityError, Target: string(key.target), Resource: rel, Message: err.Error()})
			}
			out.written = changed || extras.pending
			out.hookChanges = changes
			return out
		}
	}
	slog.Info("native hook aggregate reconciled", "target", key.target, "machine_scope", key.machineScope,
		"file", rel, "owned_entries", len(plan.owned), "satisfactions", len(plan.satisfactions),
		"aggregate_changed", changed, "additional_participants", len(extras.participants))
	out.written = changed || extras.pending
	out.hookChanges = changes
	return out
}

func unchangedNativeAggregateAssertion(abs string, raw []byte, readErr error, originalSnapshots ...*nativeHookAggregateSnapshot) (dependencytxn.DesiredFile, error) {
	live, generation, mode, err := dependencytxn.ReadSnapshot(abs)
	if err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	if len(originalSnapshots) > 0 && !nativeHookAggregateSnapshotMatches(originalSnapshots[0], live, generation, mode) {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
	}
	if errors.Is(readErr, os.ErrNotExist) {
		if generation.Exists {
			return dependencytxn.DesiredFile{}, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
		}
		return dependencytxn.DesiredFile{Path: abs, Expected: generation, Delete: true}, nil
	}
	if readErr != nil {
		return dependencytxn.DesiredFile{}, readErr
	}
	if !generation.Exists || !bytes.Equal(live, raw) {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
	}
	return dependencytxn.DesiredFile{
		Path:         abs,
		Expected:     generation,
		ExpectedMode: mode,
		Bytes:        append([]byte(nil), raw...),
		Mode:         mode,
	}, nil
}

func hasDependencyParticipant(files []dependencytxn.DesiredFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func reconcileAbsentNativeHookMetadata(ctx context.Context, out ownedJSONAggregateReport, store *ownership.Store, key outputKey, outDir string, file ownedJSONFileSpec, abs string, options ownedJSONReconcileOptions, dryRun bool) ownedJSONAggregateReport {
	rel := file.rel
	proof, proofErr := store.Proof(rel)
	out.diags = append(out.diags, ownedAggregateProofDiags(key, outDir, rel, proofErr)...)
	if proof.OwnedCount() == 0 && len(proof.HookSatisfactions()) == 0 {
		return out
	}
	if dryRun {
		return out
	}
	satisfactions := []ownership.HookSatisfaction{}
	mutation := ownership.Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		Entries:               nativeRetainedNonHookEntries(nil, proof),
		Reason:                ownership.ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
	}
	if options.batch != nil {
		if err := options.batch.addMutation(ctx, store, mutation, satisfactions); err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
			return out
		}
		if err := options.batch.addParticipant(dependencytxn.DesiredFile{Path: abs, Expected: dependencytxn.Generation{}, Delete: true}); err != nil {
			out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
		}
		return out
	}
	stateFile, err := store.Prepare(mutation, &satisfactions)
	if err != nil {
		out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
		return out
	}
	if stateFile.Path == "" {
		return out
	}
	desired := []dependencytxn.DesiredFile{
		{Path: abs, Delete: true},
		stateFile,
	}
	if _, err := commitNativeHookFiles(ctx, desired); err != nil {
		out.diags = append(out.diags, ownedAggregateCodedDiag(key, outDir, rel, err, options.writeCode))
	}
	return out
}

func nativeRetainedNonHookEntries(claimed []ownership.Entry, proof ownership.Proof, reconciledKinds ...map[ownership.EntryKind]bool) []ownership.Entry {
	var reconciled map[ownership.EntryKind]bool
	if len(reconciledKinds) > 0 {
		reconciled = reconciledKinds[0]
	}
	claimedIDs := make(map[string]bool, len(claimed))
	for _, entry := range claimed {
		claimedIDs[ownedAggregateIDKey(entry.ID())] = true
	}
	var retained []ownership.Entry
	for _, id := range proof.OwnedIDs() {
		if id.Kind == ownership.KindHook || reconciled[id.Kind] || claimedIDs[ownedAggregateIDKey(id)] {
			continue
		}
		if entry, ok := proof.Entry(id); ok {
			retained = append(retained, entry)
		}
	}
	return retained
}

func nativeOwnedJSONMutation(rel, abs string, raw []byte, readErr error, next []byte, plan ownedJSONPatch, proof ownership.Proof) ownership.Mutation {
	priorGen, priorDigest := ownedAggregatePriorForApply(raw, readErr, proof)
	entries := append(append([]ownership.Entry(nil), plan.owned...), nativeRetainedNonHookEntries(plan.owned, proof, plan.reconciledKinds)...)
	return ownership.Mutation{
		Rel: rel, AbsFile: abs, NextBytes: next, Entries: entries, Reason: ownership.ReasonSync,
		ExpectPriorGeneration: priorGen, ExpectPriorDigest: priorDigest,
	}
}

func nativeOwnedJSONTxnPlan(store *ownership.Store, rel, abs string, raw []byte, readErr error, next []byte, changed bool, plan ownedJSONPatch, proof ownership.Proof, originalSnapshots ...*nativeHookAggregateSnapshot) ([]dependencytxn.DesiredFile, error) {
	return nativeOwnedJSONTxnPlanWithBatch(context.Background(), store, rel, abs, raw, readErr, next, changed, plan, proof, nil, originalSnapshots...)
}

func nativeOwnedJSONTxnPlanWithBatch(ctx context.Context, store *ownership.Store, rel, abs string, raw []byte, readErr error, next []byte, changed bool, plan ownedJSONPatch, proof ownership.Proof, batch *pluginInstanceBatch, originalSnapshots ...*nativeHookAggregateSnapshot) ([]dependencytxn.DesiredFile, error) {
	var live []byte
	var liveGen dependencytxn.Generation
	var liveMode fs.FileMode
	if batch != nil {
		snapshot, err := batch.readAggregate(ctx, store, rel, false)
		if err != nil {
			return nil, fmt.Errorf("snapshot aggregate %q for commit: %w", rel, err)
		}
		live, liveGen, liveMode = snapshot.bytes, snapshot.generation, snapshot.mode
	} else {
		var snapErr error
		live, liveGen, liveMode, snapErr = dependencytxn.ReadSnapshot(abs)
		if snapErr != nil {
			return nil, fmt.Errorf("snapshot aggregate %q for commit: %w", rel, snapErr)
		}
	}
	var original *nativeHookAggregateSnapshot
	if len(originalSnapshots) > 0 {
		original = originalSnapshots[0]
	}
	if original != nil {
		if !nativeHookAggregateSnapshotMatches(original, live, liveGen, liveMode) {
			return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
		}
	} else if readErr == nil {
		if !liveGen.Exists || !bytes.Equal(live, raw) {
			return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
		}
	} else if errors.Is(readErr, os.ErrNotExist) {
		if liveGen.Exists {
			return nil, fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, abs)
		}
	} else if readErr != nil {
		return nil, readErr
	}
	mutation := nativeOwnedJSONMutation(rel, abs, raw, readErr, next, plan, proof)
	satisfactions := plan.satisfactions
	if batch != nil {
		if err := batch.addMutation(ctx, store, mutation, satisfactions); err != nil {
			return nil, err
		}
	} else {
		stateFile, err := store.Prepare(mutation, &satisfactions)
		if err != nil {
			return nil, err
		}
		if !changed && stateFile.Path == "" {
			return nil, nil
		}
		mode := fs.FileMode(0o600)
		var expectedMode fs.FileMode
		if liveGen.Exists {
			mode = liveMode
			expectedMode = liveMode
		}
		desired := []dependencytxn.DesiredFile{{
			Path:         abs,
			Expected:     liveGen,
			ExpectedMode: expectedMode,
			Bytes:        next,
			Mode:         mode,
		}}
		if stateFile.Path != "" {
			desired = append(desired, stateFile)
		}
		return desired, nil
	}
	mode := fs.FileMode(0o600)
	var expectedMode fs.FileMode
	if liveGen.Exists {
		mode = liveMode
		expectedMode = liveMode
	}
	desired := []dependencytxn.DesiredFile{{
		Path:         abs,
		Expected:     liveGen,
		ExpectedMode: expectedMode,
		Bytes:        next,
		Mode:         mode,
	}}
	return desired, nil
}

func commitNativeHookFiles(ctx context.Context, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return dependencytxn.Result{}, fmt.Errorf("resolve dependency transaction state: %w", err)
	}
	return dependencytxn.Commit(ctx, stateDir, desired, opts...)
}

// ownedJSONContainerNode returns the object node whose members are the aggregate's
// entries, plus the root member name it lives under:
//
//   - mcp_config.json: the `mcpServers` member of the root object.
//   - hooks.v1.json: the root object itself, which "contains the hook object
//     directly rather than nesting it under a top-level `hooks` key". Its
//     member name is "" because there is no enclosing member.
//   - config.json: the `hooks` member, so unrelated settings are untouched.
//
// A nil container with a nil error means the member does not exist yet. It is
// deliberately NOT created here: jsonc.SetMember stores raw JSON as an opaque
// literal node, so an empty object created that way could not then be patched
// member-by-member. An absent container holds no unmanaged content and no
// comments, so applyOwnedJSONPatch renders it whole instead.
func ownedJSONContainerNode(doc *jsonc.Document, file ownedJSONFileSpec) (container *jsonc.Node, member string, err error) {
	rel := file.rel
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return nil, "", fmt.Errorf("aggregate %q root is not a JSON object; it was left unchanged", rel)
	}
	member = file.container
	if member == "" {
		return root, "", nil
	}
	node, ok := root.Member(member)
	if !ok {
		return nil, member, nil
	}
	if node.Kind() != jsonc.KindObject {
		return nil, member, fmt.Errorf("aggregate %q member %q is not a JSON object; it was left unchanged", rel, member)
	}
	return node, member, nil
}

func ownedJSONScalar(raw []byte) bool {
	var value any
	if err := ownedJSONDecode(raw, &value); err != nil {
		return false
	}
	switch value.(type) {
	case nil, bool, string, json.Number:
		return true
	default:
		return false
	}
}

func validateOwnedJSONEnvelope(doc *jsonc.Document, file ownedJSONFileSpec, allowMissing bool) error {
	if len(file.envelope) == 0 {
		return nil
	}
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return fmt.Errorf("aggregate %q root is not a JSON object; it was left unchanged", file.rel)
	}
	keys := make([]string, 0, len(file.envelope))
	for key := range file.envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		expectedRaw, err := json.Marshal(file.envelope[key])
		if err != nil || !ownedJSONScalar(expectedRaw) {
			return fmt.Errorf("aggregate %q declares a non-scalar envelope member %q", file.rel, key)
		}
		member, present := root.Member(key)
		if !present {
			if !allowMissing {
				return fmt.Errorf("aggregate %q is missing declared envelope member %q; it was left unchanged", file.rel, key)
			}
			if !doc.SetMember(root, key, expectedRaw) {
				return fmt.Errorf("aggregate %q could not create declared envelope member %q", file.rel, key)
			}
			continue
		}
		actualRaw := member.Raw()
		if !ownedJSONScalar(actualRaw) {
			return fmt.Errorf("aggregate %q envelope member %q is not a scalar; it was left unchanged", file.rel, key)
		}
		actual, err := canonicalOwnedJSON(actualRaw)
		if err != nil {
			return fmt.Errorf("aggregate %q envelope member %q is invalid; it was left unchanged", file.rel, key)
		}
		expected, err := canonicalOwnedJSON(expectedRaw)
		if err != nil || !bytes.Equal(actual, expected) {
			return fmt.Errorf("aggregate %q envelope member %q differs from its declaration; it was left unchanged", file.rel, key)
		}
	}
	return nil
}

func validateOwnedJSONHookRule(shape targetmeta.HookShape, rule *jsonc.Node) error {
	if rule == nil || rule.Kind() != jsonc.KindObject {
		return fmt.Errorf("hook entry is not an object")
	}
	switch shape {
	case targetmeta.HookShapeEventHandlers:
		return nil
	case targetmeta.HookShapeMatcherGroups:
		handlers, ok := rule.Member("hooks")
		if !ok || handlers.Kind() != jsonc.KindArray {
			return fmt.Errorf("matcher group does not contain a hooks array")
		}
		for i := 0; i < handlers.Len(); i++ {
			handler, _ := handlers.Element(i)
			if handler.Kind() != jsonc.KindObject {
				return fmt.Errorf("matcher group contains a non-object handler")
			}
		}
		return nil
	default:
		return fmt.Errorf("hook aggregate declares an unsupported entry shape")
	}
}

func validateOwnedJSONDesiredHooks(file ownedJSONFileSpec, desired []ownedJSONEntry) error {
	if file.hookShape == "" {
		return nil
	}
	for _, entry := range desired {
		if entry.rel != file.rel || entry.container == "" {
			return fmt.Errorf("aggregate %q has an invalid desired hook entry", file.rel)
		}
		doc, err := jsonc.Parse(file.rel, entry.raw)
		if err != nil {
			return fmt.Errorf("aggregate %q desired hook entry is invalid", file.rel)
		}
		if err := validateOwnedJSONHookRule(file.hookShape, doc.Root()); err != nil {
			return fmt.Errorf("aggregate %q desired hook entry does not match its declared shape", file.rel)
		}
	}
	return nil
}

func ensureOwnedJSONHookParent(outDir, rel string) error {
	root, err := ownership.CanonicalRoot(outDir)
	if err != nil {
		return fmt.Errorf("aggregate root is unsafe: %w", err)
	}
	parent := filepath.Dir(filepath.Join(outDir, filepath.FromSlash(rel)))
	canonicalParent, err := ownership.CanonicalRoot(parent)
	if err != nil {
		return fmt.Errorf("aggregate parent is unsafe: %w", err)
	}
	if canonicalParent != filepath.Clean(parent) {
		return fmt.Errorf("aggregate parent contains a symlink")
	}
	relative, err := filepath.Rel(root, canonicalParent)
	if err != nil || !fs.ValidPath(filepath.ToSlash(relative)) {
		return fmt.Errorf("aggregate parent is outside its owned root")
	}
	if info, statErr := os.Stat(canonicalParent); statErr == nil && !info.IsDir() || statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("aggregate parent is not a safe directory")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create aggregate parent: %w", err)
	}
	return nil
}

// ownedJSONExistingEntry pairs an on-disk entry's ownership identity with where it
// lives, so a patch can address it without re-deriving anything.
type ownedJSONExistingEntry struct {
	value ownership.EntryValue
	// container is the hook event name or "mcpServers".
	container string
	// raw is the entry's EXACT on-disk bytes. Preserving an unmanaged entry
	// means writing these bytes back, not a re-serialization of them.
	raw []byte
}

// ownedJSONExistingEntries reads the current entries out of the container node.
func ownedJSONExistingEntries(file ownedJSONFileSpec, container *jsonc.Node, legacyProofs ...ownership.Proof) ([]ownedJSONExistingEntry, error) {
	rel := file.rel
	if len(legacyProofs) > 1 {
		return nil, fmt.Errorf("aggregate %q accepts at most one legacy ownership proof", rel)
	}
	var legacyProof ownership.Proof
	if len(legacyProofs) == 1 {
		legacyProof = legacyProofs[0]
	}
	if file.keyed {
		var out []ownedJSONExistingEntry
		for _, key := range container.Keys() {
			node, _ := container.Member(key)
			canonical, err := jsonc.Canonicalize(rel, node.Raw())
			if err != nil {
				return nil, fmt.Errorf("aggregate %q entry %q is not valid JSON; it was left unchanged", rel, key)
			}
			normalized, err := canonicalOwnedJSON(canonical)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q entry %q could not be normalized; it was left unchanged", rel, key)
			}
			out = append(out, ownedJSONExistingEntry{
				value: ownership.EntryValue{
					ID:          ownership.EntryID{Kind: ownership.KindMcpServer, Container: file.container, Key: key, IdentityDigest: ownership.McpIdentityDigest(key)},
					ValueDigest: ownership.Digest(normalized),
				},
				container: file.container,
				raw:       node.Raw(),
			})
		}
		return out, nil
	}
	var out []ownedJSONExistingEntry
	for _, event := range container.Keys() {
		node, _ := container.Member(event)
		if node.Kind() != jsonc.KindArray {
			if file.hookShape != "" {
				return nil, fmt.Errorf("aggregate %q event %q is not an array of declared hook entries; it was left unchanged", rel, event)
			}
			// A non-array hook event is not a shape this target writes. It is
			// left alone rather than rewritten, and it contributes no entries,
			// so nothing about it can be pruned either.
			slog.Warn("owned JSON hook event is not an array; it was left unchanged",
				"file", rel, "event", event)
			continue
		}
		for i := 0; i < node.Len(); i++ {
			element, _ := node.Element(i)
			canonical, err := jsonc.Canonicalize(rel, element.Raw())
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d is not valid JSON; it was left unchanged", rel, event, i)
			}
			normalized, err := canonicalOwnedJSON(canonical)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d could not be normalized; it was left unchanged", rel, event, i)
			}
			identity, err := ownedJSONHookIdentity(event, normalized)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d has no derivable identity; it was left unchanged", rel, event, i)
			}
			valueDigest := ownership.Digest(normalized)
			if file.rawValueDigest {
				valueDigest = ownership.Digest(element.Raw())
			}
			value := ownership.EntryValue{
				ID:          ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: identity},
				ValueDigest: valueDigest,
			}
			if file.hookShape != "" {
				if err := validateOwnedJSONHookRule(file.hookShape, element); err != nil &&
					!ownedJSONLegacyFlatHookRuleIsOwned(file, element, value, legacyProof) {
					return nil, fmt.Errorf("aggregate %q event %q entry %d does not match its declared hook shape; it was left unchanged", rel, event, i)
				}
			}
			out = append(out, ownedJSONExistingEntry{
				value:     value,
				container: event,
				raw:       element.Raw(),
			})
		}
	}
	return out, nil
}

func ownedJSONLegacyFlatHookRuleIsOwned(file ownedJSONFileSpec, rule *jsonc.Node, value ownership.EntryValue, proof ownership.Proof) bool {
	if file.hookShape != targetmeta.HookShapeMatcherGroups || rule == nil || rule.Kind() != jsonc.KindObject {
		return false
	}
	for _, key := range rule.Keys() {
		if strings.EqualFold(key, "hooks") {
			return false
		}
	}
	return proof.Classify(value.ID, value.ValueDigest) == ownership.StatusOwnedClean
}

// ownedJSONPatch is the fully decided set of edits for one aggregate file. It is
// computed before any mutation so the decision logic is testable without a
// filesystem, and so a refused collision can never be half-applied.
type ownedJSONPatch struct {
	// outDir is the install root the aggregate lives under. It exists purely so
	// every reported decision can name which aggregate it is about. No patching
	// decision reads it.
	outDir string
	rel    string
	// write maps a container key ("mcpServers" key, or hook event) to the entry
	// bytes to store there.
	writeMcp map[string][]byte
	// hookElements maps a hook event to its complete new element list:
	// preserved handwritten elements first, in their original order, then
	// generated ones in deterministic resource order.
	hookElements map[string][][]byte
	// deleteMcp / deleteHookEvent are keys whose entries are pruned entirely.
	deleteMcp       []string
	deleteHookEvent []string
	// owned is the entry set the new content claims: what ownership state
	// records for the next generation.
	owned []ownership.Entry

	collisions      []ownership.EntryID
	drifted         []ownership.EntryID
	prune           []ownership.EntryID
	satisfied       []ownedJSONEntry
	satisfactions   []ownership.HookSatisfaction
	prunePostponed  []ownership.EntryID
	pruneSuppressed bool
	reconciledKinds map[ownership.EntryKind]bool
}

// ownedJSONPatchPlan decides every edit for one file.
//
// The safety rules it implements, each quoted from the plan:
//
//   - "An unmanaged MCP key collision is preserved and causes only the
//     generated entry to fail" — a colliding desired key is dropped from the
//     patch and reported; the existing member is never written or deleted.
//   - "Manual edits to a proven owned entry are reported as drift and replaced
//     from marketplace configuration."
//   - "Stale proven owned entries are pruned" — and ONLY those: the prune list
//     is DriftReport.Stale, which ownership.Classify derives from Proof.OwnedIDs.
//     No filesystem or content heuristic contributes to it.
//   - "One semantically identical unmanaged hook may satisfy any number of
//     selected marketplace resources without duplication or adoption."
//   - "Handwritten hooks keep their relative order and execute before generated
//     hooks."
//   - "A partial run does not prune" — when incomplete is set, the prune list is
//     emptied for this file.
func ownedJSONPatchPlan(outDir string, file ownedJSONFileSpec, desired []ownedJSONEntry, existing []ownedJSONExistingEntry, drift ownership.DriftReport, proof ownership.Proof, incomplete bool, selected ...map[string]bool) ownedJSONPatch {
	var selection map[string]bool
	if len(selected) > 0 {
		selection = selected[0]
	}
	rel := file.rel
	patch := ownedJSONPatch{outDir: outDir, rel: rel, writeMcp: map[string][]byte{}, hookElements: map[string][][]byte{}}

	collision := make(map[string]bool, len(drift.Collisions))
	for _, id := range drift.Collisions {
		collision[ownedAggregateIDKey(id)] = true
	}
	drifted := make(map[string]bool, len(drift.Drifted))
	for _, id := range drift.Drifted {
		drifted[ownedAggregateIDKey(id)] = true
	}
	stale := make(map[string]bool, len(drift.Stale))
	postponedStale := drift.Stale
	for _, id := range drift.Stale {
		stale[ownedAggregateIDKey(id)] = true
	}
	if incomplete {
		allowed, postponed := partitionIncompleteStale(drift.Stale, proof, selection)
		patch.prune = allowed
		patch.prunePostponed = postponed
		patch.pruneSuppressed = len(postponed) > 0
		postponedStale = postponed
		stale = make(map[string]bool, len(allowed))
		for _, id := range allowed {
			stale[ownedAggregateIDKey(id)] = true
		}
	} else {
		patch.prune = drift.Stale
	}
	patch.drifted = drift.Drifted
	if file.keyed {
		// Only a KEYED primitive can genuinely collide: "An unmanaged MCP key
		// collision is preserved and causes only the generated entry to fail."
		// A hook has no key, so an existing unmanaged entry with the same
		// identity IS the same behaviour and SATISFIES the desired entry rather
		// than conflicting with it (see the hook branch below).
		patch.collisions = drift.Collisions
	}

	if file.keyed {
		for _, entry := range desired {
			if collision[ownedAggregateIDKey(entry.id())] {
				continue // preserved unmanaged entry wins; only ours fails
			}
			patch.writeMcp[entry.key] = entry.raw
			patch.owned = append(patch.owned, entry.ownershipEntry())
		}
		for _, id := range drift.Stale {
			if stale[ownedAggregateIDKey(id)] {
				patch.deleteMcp = append(patch.deleteMcp, id.Key)
			}
		}
		retainPostponedOwnership(&patch, existing, postponedStale, proof, incomplete)
		return patch
	}

	// Hooks. Identity is the event plus the digest of the canonical hook value,
	// so a desired entry whose identity already exists UNMANAGED is byte-identical
	// to that handwritten entry and is satisfied by it rather than being a
	// collision: adding ours would duplicate the behaviour.
	unmanagedIdentities := make(map[string]bool, len(existing))
	for _, id := range drift.Unmanaged {
		unmanagedIdentities[ownedAggregateIDKey(id)] = true
	}

	desiredByEvent := make(map[string][]ownedJSONEntry)
	events := map[string]bool{}
	for _, entry := range desired {
		if unmanagedIdentities[ownedAggregateIDKey(entry.id())] {
			patch.satisfied = append(patch.satisfied, entry)
			continue
		}
		satisfied := false
		for _, existingEntry := range existing {
			if existingEntry.container != entry.container || !unmanagedIdentities[ownedAggregateIDKey(existingEntry.value.ID)] {
				continue
			}
			existingRaw, err := canonicalOwnedJSON(existingEntry.raw)
			if err != nil {
				continue
			}
			existingValue, ok := nativeHookValueWithoutOwnershipMarker(existingRaw)
			if !ok || !bytes.Equal(existingValue, entry.raw) {
				continue
			}
			satisfiedEntry := entry
			satisfiedEntry.raw = existingRaw
			satisfiedEntry.identity = existingEntry.value.ID.IdentityDigest
			patch.satisfied = append(patch.satisfied, satisfiedEntry)
			satisfied = true
			break
		}
		if satisfied {
			continue
		}
		desiredByEvent[entry.container] = append(desiredByEvent[entry.container], entry)
		events[entry.container] = true
	}
	existingByEvent := make(map[string][]ownedJSONExistingEntry)
	for _, entry := range existing {
		existingByEvent[entry.container] = append(existingByEvent[entry.container], entry)
		events[entry.container] = true
	}

	for _, event := range sortedKeys(events) {
		var elements [][]byte
		for _, entry := range existingByEvent[event] {
			id := ownedAggregateIDKey(entry.value.ID)
			if stale[id] {
				continue // proven ours and no longer desired: pruned
			}
			if drifted[id] {
				continue // proven ours and hand-edited: replaced below
			}
			if ownedJSONDesires(desiredByEvent[event], entry.value.ID) {
				// Proven ours and still desired: the desired copy is appended
				// below, after every handwritten element, because "Handwritten
				// hooks keep their relative order and execute before generated
				// hooks".
				continue
			}
			// Unmanaged — preserve the EXACT source bytes, so handwritten
			// formatting and any comment attached to this element survive.
			elements = append(elements, entry.raw)
		}
		for _, entry := range desiredByEvent[event] {
			elements = append(elements, entry.raw)
			patch.owned = append(patch.owned, entry.ownershipEntry())
		}
		if len(elements) == 0 {
			patch.deleteHookEvent = append(patch.deleteHookEvent, event)
			continue
		}
		patch.hookElements[event] = elements
	}
	patch.satisfactions = desiredHookSatisfactions(patch.satisfied)
	if incomplete {
		patch.satisfactions = mergeRetainedHookSatisfactions(patch.satisfactions, proof.HookSatisfactions(), existing, proof)
	}
	sort.Slice(patch.satisfactions, func(i, j int) bool {
		a, b := patch.satisfactions[i], patch.satisfactions[j]
		return a.Container < b.Container || a.Container == b.Container && a.IdentityDigest < b.IdentityDigest
	})
	retainPostponedOwnership(&patch, existing, postponedStale, proof, incomplete)
	return patch
}

func nativeHookValueWithoutOwnershipMarker(raw []byte) ([]byte, bool) {
	var fields map[string]json.RawMessage
	if err := ownedJSONDecode(raw, &fields); err != nil || fields == nil {
		return nil, false
	}
	removedMarker := false
	for key := range fields {
		if strings.EqualFold(key, "_managed_by") {
			delete(fields, key)
			removedMarker = true
		}
	}
	if !removedMarker {
		return nil, false
	}
	withoutMarker, err := json.Marshal(fields)
	if err != nil {
		return nil, false
	}
	canonical, err := canonicalOwnedJSON(withoutMarker)
	if err != nil {
		return nil, false
	}
	return canonical, true
}

func exactSourcePairs(entry ownedJSONEntry) []ownership.HookSourceEvidence {
	var pairs []ownership.HookSourceEvidence
	if entry.sourceDigest != "" {
		pairs = append(pairs, ownership.HookSourceEvidence{Resource: entry.resource, SourceDigest: entry.sourceDigest})
	}
	pairs = append(pairs, entry.sourceEvidence...)
	return mergeSourcePairs(pairs, nil)
}

func mergeSourcePairs(a, b []ownership.HookSourceEvidence) []ownership.HookSourceEvidence {
	pairs := make([]ownership.HookSourceEvidence, 0, len(a)+len(b))
	pairs = append(pairs, a...)
	pairs = append(pairs, b...)
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Resource != pairs[j].Resource {
			return pairs[i].Resource < pairs[j].Resource
		}
		return pairs[i].SourceDigest < pairs[j].SourceDigest
	})
	unique := pairs[:0]
	for _, pair := range pairs {
		if len(unique) == 0 || unique[len(unique)-1] != pair {
			unique = append(unique, pair)
		}
	}
	return unique
}

func desiredHookSatisfactions(satisfied []ownedJSONEntry) []ownership.HookSatisfaction {
	var out []ownership.HookSatisfaction
	for _, entry := range satisfied {
		sources := exactSourcePairs(entry)
		if len(sources) == 0 {
			continue
		}
		out = append(out, ownership.HookSatisfaction{
			Container:      entry.container,
			IdentityDigest: entry.identity,
			ValueDigest:    entry.valueDigest(),
			PluginInstance: entry.pluginInstanceKey,
			Sources:        sources,
		})
	}
	return out
}

func mergeRetainedHookSatisfactions(current, prior []ownership.HookSatisfaction, existing []ownedJSONExistingEntry, proof ownership.Proof) []ownership.HookSatisfaction {
	out := append([]ownership.HookSatisfaction(nil), current...)
	index := make(map[string]int, len(out))
	for i, satisfaction := range out {
		index[ownedAggregateIDKey(satisfaction.ID())] = i
	}
	for _, record := range prior {
		retained := false
		for _, entry := range existing {
			if ownedAggregateIDKey(entry.value.ID) != ownedAggregateIDKey(record.ID()) ||
				entry.value.ValueDigest != record.ValueDigest {
				continue
			}
			if proof.Classify(entry.value.ID, entry.value.ValueDigest) == ownership.StatusUnmanaged {
				retained = true
			}
			break
		}
		if !retained {
			continue
		}
		key := ownedAggregateIDKey(record.ID())
		if i, ok := index[key]; ok {
			out[i].Sources = mergeSourcePairs(out[i].Sources, record.Sources)
			continue
		}
		merged := record
		merged.Sources = append([]ownership.HookSourceEvidence(nil), record.Sources...)
		index[key] = len(out)
		out = append(out, merged)
	}
	return out
}

func partitionIncompleteStale(stale []ownership.EntryID, proof ownership.Proof, selected map[string]bool) (allowed, postponed []ownership.EntryID) {
	for _, id := range stale {
		if selectivePruneProven(id, proof, selected) {
			allowed = append(allowed, id)
			continue
		}
		postponed = append(postponed, id)
	}
	return allowed, postponed
}

func selectivePruneProven(id ownership.EntryID, proof ownership.Proof, selected map[string]bool) bool {
	if len(selected) == 0 || id.Kind != ownership.KindHook {
		return false
	}
	entry, ok := proof.Entry(id)
	if !ok || entry.Resource == "" {
		return false
	}
	if selected[entry.Resource] {
		return false
	}
	for _, evidence := range entry.SourceEvidence {
		if evidence.Resource == "" || selected[evidence.Resource] {
			return false
		}
	}
	return true
}

func retainPostponedOwnership(patch *ownedJSONPatch, existing []ownedJSONExistingEntry, stale []ownership.EntryID, proof ownership.Proof, incomplete bool) {
	if !incomplete {
		return
	}
	for _, entry := range existing {
		if !containsOwnershipID(stale, entry.value.ID) {
			continue
		}
		if owned, ok := proof.Entry(entry.value.ID); ok {
			patch.owned = append(patch.owned, owned)
		}
	}
}

func containsOwnershipID(ids []ownership.EntryID, want ownership.EntryID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// ownedJSONDesires reports whether an identity is in a desired slice.
func ownedJSONDesires(desired []ownedJSONEntry, id ownership.EntryID) bool {
	want := ownedAggregateIDKey(id)
	for _, entry := range desired {
		if ownedAggregateIDKey(entry.id()) == want {
			return true
		}
	}
	return false
}

// applyOwnedJSONPatch applies a decided patch to the parsed document. Every edit
// goes through pkg/harness-compilation/jsonc, so untouched members keep their
// exact bytes, their comments, and their position.
func applyOwnedJSONPatch(doc *jsonc.Document, container *jsonc.Node, containerMember string, file ownedJSONFileSpec, patch ownedJSONPatch) error {
	rel := file.rel
	if container == nil {
		// The container member does not exist yet, so the whole member is
		// rendered from the patch. Nothing handwritten can be lost: there was
		// nothing inside it to preserve.
		raw, err := freshOwnedJSONContainerRaw(file, patch)
		if err != nil {
			return err
		}
		if raw == nil {
			return nil
		}
		if !doc.SetMember(doc.Root(), containerMember, raw) {
			return fmt.Errorf("aggregate %q could not create member %q", rel, containerMember)
		}
		return nil
	}
	if file.keyed {
		for _, key := range sortedKeys(patch.writeMcp) {
			if !doc.SetMember(container, key, patch.writeMcp[key]) {
				return fmt.Errorf("aggregate %q could not set member %q", rel, key)
			}
		}
		for _, key := range patch.deleteMcp {
			doc.DeleteMember(container, key)
		}
		return nil
	}
	for _, event := range sortedKeys(patch.hookElements) {
		elements := patch.hookElements[event]
		node, ok := container.Member(event)
		if !ok || node.Kind() != jsonc.KindArray {
			raw, err := ownedJSONHookArrayRaw(elements)
			if err != nil {
				return err
			}
			if !doc.SetMember(container, event, raw) {
				return fmt.Errorf("aggregate %q could not set hook event %q", rel, event)
			}
			continue
		}
		if !doc.SetElements(node, elements) {
			return fmt.Errorf("aggregate %q could not set hook event %q elements", rel, event)
		}
	}
	for _, event := range patch.deleteHookEvent {
		doc.DeleteMember(container, event)
	}
	return nil
}

// freshOwnedJSONContainerRaw renders a whole absent container member from a patch.
// Everything in the patch is generated content, so the result is canonical JSON.
func freshOwnedJSONContainerRaw(file ownedJSONFileSpec, patch ownedJSONPatch) ([]byte, error) {
	rel := file.rel
	var buf bytes.Buffer
	buf.WriteByte('{')
	written := 0
	writeKey := func(key string) error {
		if written > 0 {
			buf.WriteByte(',')
		}
		written++
		keyRaw, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf.Write(keyRaw)
		buf.WriteByte(':')
		return nil
	}
	if file.keyed {
		for _, key := range sortedKeys(patch.writeMcp) {
			if err := writeKey(key); err != nil {
				return nil, err
			}
			buf.Write(patch.writeMcp[key])
		}
	} else {
		for _, event := range sortedKeys(patch.hookElements) {
			if err := writeKey(event); err != nil {
				return nil, err
			}
			raw, err := ownedJSONHookArrayRaw(patch.hookElements[event])
			if err != nil {
				return nil, err
			}
			buf.Write(raw)
		}
	}
	buf.WriteByte('}')
	if written == 0 {
		return nil, nil
	}
	if !json.Valid(buf.Bytes()) {
		return nil, fmt.Errorf("aggregate %q generated container is not valid JSON", rel)
	}
	return buf.Bytes(), nil
}

// ownedJSONHookArrayRaw renders a fresh hook event array. It is only used when the
// event member does not exist yet, so no handwritten formatting is at stake.
func ownedJSONHookArrayRaw(elements [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, element := range elements {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(element)
	}
	buf.WriteByte(']')
	if !json.Valid(buf.Bytes()) {
		return nil, errors.New("generated hook array is not valid JSON")
	}
	return buf.Bytes(), nil
}

// diags renders the patch's decisions as diagnostics. Exactly one code per
// decision class, and nothing but identities and file paths in the messages.
//
// Every message names the aggregate through [ownedAggregateLabel], so a user
// can distinguish the scope, destination root, and sibling aggregate files.
func (p ownedJSONPatch) diags(key outputKey, options ownedJSONReconcileOptions) []diag.Diagnostic {
	var out []diag.Diagnostic
	file := ownedAggregateLabel(p.outDir, p.rel)
	for _, id := range p.collisions {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeOwnershipUnmanagedKey,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("collision: %s entry %q in %q already exists and is not proven owned; the existing entry was preserved and only the generated entry failed",
				id.Kind, ownedAggregateIdentityLabel(id), file),
			Hint: "Rename the marketplace resource, or run doctor --repair to adopt the existing entry when it matches exactly",
		})
	}
	for _, id := range p.drifted {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeOwnershipEntryDrift,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("drift: %s entry %q in %q was edited by hand and was replaced from marketplace configuration",
				id.Kind, ownedAggregateIdentityLabel(id), file),
			Hint: "Edit the marketplace resource instead: an owned entry is regenerated on every sync",
		})
	}
	for _, entry := range p.satisfied {
		out = append(out, diag.Diagnostic{
			Code:     options.satisfiedCode,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Resource: entry.resource,
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("a handwritten %q hook in %q is semantically identical to this resource's hook; no generated entry was added",
				entry.container, file),
			Hint: "No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated",
		})
	}
	if p.pruneSuppressed && len(p.prunePostponed) > 0 {
		out = append(out, diag.Diagnostic{
			Code:     options.prunePostponedCode,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("%d stale owned entries in %q were not pruned because a resource was skipped this run",
				len(p.prunePostponed), file),
			Hint: "Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one",
		})
	}
	return out
}
