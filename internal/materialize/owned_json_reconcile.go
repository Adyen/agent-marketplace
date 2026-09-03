package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
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
	written bool
	diags   []diag.Diagnostic
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
	for _, file := range files {
		if _, err := os.Lstat(filepath.Join(outDir, file.rel)); err == nil {
			anyWork = true
		}
	}
	if !anyWork {
		return report
	}
	if !dryRun {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "", fmt.Sprintf("create aggregate root %s: %v", outDir, err)))
			return report
		}
	}

	// The observer is how a WAIT reaches the report. A contended acquisition that
	// eventually succeeds returns no error, so without it the user is never told
	// why their sync paused; and a degradation is a property of the root that must
	// be reported once, not once per file.
	lockReport := newOwnedAggregateLockReporter(key, outDir)
	store, err := ownership.Open(outDir, ownership.WithLockObserver(lockReport.observe))
	if err != nil {
		report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "",
			fmt.Sprintf("open ownership store for %s: %v", outDir, err)))
		return report
	}

	// Dry run: no session, no lock, no state-root writes. See the doc comment.
	if dryRun {
		return appendOwnedJSONAggregateReports(report, store, key, outDir, files, desiredByRel, work, options, true)
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
		report = appendOwnedJSONAggregateReports(report, store, key, outDir, files, desiredByRel, work, options, false)
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
	report.Diags = append(report.Diags, lockReport.diags()...)
	return report
}

// appendOwnedJSONAggregateReports runs the per-file loop and folds each file's outcome
// into the report. It is a function rather than an inline loop only so the dry-run
// path can reuse it verbatim without a session around it.
func appendOwnedJSONAggregateReports(report *Report, store *ownership.Store, key outputKey, outDir string, files []ownedJSONFileSpec,
	desiredByRel map[string][]ownedJSONEntry, work *ownedJSONAggregateWork, options ownedJSONReconcileOptions, dryRun bool) *Report {
	for _, file := range files {
		fileReport := reconcileOwnedJSONAggregateFile(store, key, outDir, file, desiredByRel[file.rel], work != nil && work.incomplete[file.rel], options, dryRun)
		report.Diags = append(report.Diags, fileReport.diags...)
		if !fileReport.written {
			continue
		}
		if options.onWrite != nil {
			options.onWrite(report, key, file.rel, dryRun)
		}
		recordOwnedAggregateWrite(report, key, options.rootID, outDir, file.rel)
	}
	return report
}

func reconcileOwnedJSONAggregateFile(store *ownership.Store, key outputKey, outDir string, file ownedJSONFileSpec, desired []ownedJSONEntry, incomplete bool, options ownedJSONReconcileOptions, dryRun bool) ownedJSONAggregateReport {
	var out ownedJSONAggregateReport
	rel := file.rel
	abs := filepath.Join(outDir, rel)
	if err := store.ValidateAggregateFile(rel, abs); err != nil {
		out.diags = append(out.diags, ownedAggregateDiag(options.writeCode, key, rel,
			fmt.Sprintf("aggregate %q is not a safe regular file: %v", ownedAggregateLabel(outDir, rel), err)))
		return out
	}

	raw, readErr := os.ReadFile(abs)
	switch {
	case readErr == nil:
	case os.IsNotExist(readErr):
		if len(desired) == 0 {
			return out
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

	container, containerMember, shapeErr := ownedJSONContainerNode(doc, file)
	if shapeErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, shapeErr))
		return out
	}
	if container == nil && len(desired) == 0 {
		// The container does not exist and nothing is desired, so there is
		// neither anything to write nor anything that could be stale.
		return out
	}

	var existing []ownedJSONExistingEntry
	if container != nil {
		existing, shapeErr = ownedJSONExistingEntries(file, container)
		if shapeErr != nil {
			out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, shapeErr))
			return out
		}
	}

	proof, proofErr := store.Proof(rel)
	// Advisory: the zero Proof already classifies everything as unmanaged, so
	// reconciliation continues safely (preserve, refuse, never prune) whether or
	// not the situation is worth reporting.
	out.diags = append(out.diags, ownedAggregateProofDiags(key, outDir, rel, proofErr)...)

	desiredValues := make([]ownership.EntryValue, 0, len(desired))
	for _, entry := range desired {
		desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
	}
	existingValues := make([]ownership.EntryValue, 0, len(existing))
	for _, entry := range existing {
		existingValues = append(existingValues, entry.value)
	}
	drift := ownership.Classify(proof, existingValues, desiredValues)

	plan := ownedJSONPatchPlan(outDir, file, desired, existing, drift, proof, incomplete)
	out.diags = append(out.diags, plan.diags(key, options)...)

	if applyErr := applyOwnedJSONPatch(doc, container, containerMember, file, plan); applyErr != nil {
		out.diags = append(out.diags, ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, applyErr))
		return out
	}
	if !doc.Changed() {
		slog.Debug("owned JSON aggregate already in sync", "target", key.target, "machine_scope", key.machineScope,
			"file", rel, "owned_entries", len(plan.owned))
		return out
	}
	next := doc.Bytes()
	if dryRun {
		out.written = true
		return out
	}

	priorGen, priorDigest := ownedAggregatePriorForApply(raw, readErr, proof)
	var priorMode os.FileMode
	if readErr == nil {
		info, statErr := os.Stat(abs)
		if statErr != nil {
			out.diags = append(out.diags, ownedAggregateDiag(options.writeCode, key, rel,
				fmt.Sprintf("stat aggregate %q before apply: %v", ownedAggregateLabel(outDir, rel), statErr)))
			return out
		}
		priorMode = info.Mode().Perm()
	}
	result, applyErr := store.Apply(ownership.Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		NextBytes:             next,
		Entries:               plan.owned,
		Reason:                ownership.ReasonSync,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
		ExpectPriorMode:       priorMode,
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
func ownedJSONExistingEntries(file ownedJSONFileSpec, container *jsonc.Node) ([]ownedJSONExistingEntry, error) {
	rel := file.rel
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
			out = append(out, ownedJSONExistingEntry{
				value: ownership.EntryValue{
					ID:          ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: identity},
					ValueDigest: valueDigest,
				},
				container: event,
				raw:       element.Raw(),
			})
		}
	}
	return out, nil
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
	prunePostponed  []ownership.EntryID
	pruneSuppressed bool
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
func ownedJSONPatchPlan(outDir string, file ownedJSONFileSpec, desired []ownedJSONEntry, existing []ownedJSONExistingEntry, drift ownership.DriftReport, proof ownership.Proof, incomplete bool) ownedJSONPatch {
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
	for _, id := range drift.Stale {
		stale[ownedAggregateIDKey(id)] = true
	}
	if incomplete {
		patch.pruneSuppressed = true
		patch.prunePostponed = drift.Stale
		stale = map[string]bool{}
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
		retainPostponedOwnership(&patch, existing, drift.Stale, proof, incomplete)
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
	retainPostponedOwnership(&patch, existing, drift.Stale, proof, incomplete)
	return patch
}

func retainPostponedOwnership(patch *ownedJSONPatch, existing []ownedJSONExistingEntry, stale []ownership.EntryID, proof ownership.Proof, incomplete bool) {
	if !incomplete {
		return
	}
	for _, entry := range existing {
		if !containsOwnershipID(stale, entry.value.ID) {
			continue
		}
		resource, _ := proof.Resource(entry.value.ID)
		patch.owned = append(patch.owned, ownership.Entry{Kind: entry.value.ID.Kind, Container: entry.value.ID.Container, Key: entry.value.ID.Key, IdentityDigest: entry.value.ID.IdentityDigest, ValueDigest: entry.value.ValueDigest, Resource: resource})
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
