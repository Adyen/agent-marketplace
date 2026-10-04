package materialize

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// ownedJSONRepairState is the parsed snapshot behind the JSON repair adapter.
type ownedJSONRepairState struct {
	raw             []byte
	doc             *jsonc.Document
	container       *jsonc.Node
	containerMember string
	existing        []ownedJSONExistingEntry
	existingValues  []ownership.EntryValue
	proof           ownership.Proof
}

type ownedJSONRepairAdapter struct {
	file    ownedJSONFileSpec
	options ownedJSONReconcileOptions
	redact  bool
}

func (a ownedJSONRepairAdapter) read(store *ownership.Store, key outputKey, outDir, _ string) (*repairAggregateState, []diag.Diagnostic) {
	document, diagnostics := readOwnedJSONRepairState(store, key, outDir, a.file, a.options)
	if document == nil {
		return nil, diagnostics
	}
	state := &repairAggregateState{
		raw:      document.raw,
		existing: document.existingValues,
		proof:    document.proof,
	}
	if document.container != nil {
		state.remove = func(id ownership.EntryID, digest string) ([]byte, error) {
			if err := removeOwnedJSONEntry(document, a.file, id, digest); err != nil {
				return nil, err
			}
			if !document.doc.Changed() {
				return nil, fmt.Errorf("aggregate %q produced no edit", a.file.rel)
			}
			return document.doc.Bytes(), nil
		}
		if a.file.keyed {
			state.replace = func(id ownership.EntryID, replacement []byte) ([]byte, error) {
				if err := replaceOwnedJSONEntry(document, a.file.rel, id, replacement); err != nil {
					return nil, err
				}
				if !document.doc.Changed() {
					return nil, fmt.Errorf("aggregate %q produced no edit", a.file.rel)
				}
				return document.doc.Bytes(), nil
			}
		}
	}
	return state, diagnostics
}

// previewOwnedJSONAggregateFile computes repair candidates for one JSON
// aggregate without mutating it.
func previewOwnedJSONAggregateFile(store *ownership.Store, key outputKey, outDir string, fileSpec ownedJSONFileSpec, options ownedJSONReconcileOptions, redact bool, desired []ownedJSONEntry) (*RepairFile, []diag.Diagnostic) {
	adapter := ownedJSONRepairAdapter{file: fileSpec, options: options, redact: redact}
	state, diagnostics := adapter.read(store, key, outDir, fileSpec.rel)
	if state == nil {
		return nil, diagnostics
	}

	desiredEntries := repairDesiredFromOwnedJSON(desired)
	desiredValues := repairDesiredValues(desiredEntries)
	resourceByID := make(map[string]string, len(desired))
	for _, entry := range desired {
		resourceByID[ownedAggregateIDKey(entry.id())] = entry.resource
	}

	file := &RepairFile{
		Target:          key.target,
		MachineScope:    key.machineScope,
		OutDir:          outDir,
		Rel:             fileSpec.rel,
		ExistingEntries: len(state.existing),
		DesiredEntries:  len(desiredValues),
		store:           store,
		adapter:         adapter,
		desired:         desiredEntries,
	}
	for _, candidate := range ownership.ExactAdoptionCandidates(state.proof, state.existing, desiredValues) {
		file.Adoptions = append(file.Adoptions, AdoptionCandidate{
			Candidate: candidate,
			Resource:  resourceByID[ownedAggregateIDKey(candidate.EntryID())],
		})
	}

	orphans, err := store.OrphanCandidates(state.proof, fileSpec.rel, state.existing, desiredValues)
	if err != nil {
		diagnostics = append(diagnostics, ownedAggregateCodedDiag(key, outDir, fileSpec.rel, err, options.writeCode))
	}
	for _, candidate := range orphans {
		file.Orphans = append(file.Orphans, OrphanCandidate{Candidate: candidate})
	}
	for _, candidate := range ownership.ValueConflictCandidates(state.proof, state.existing, desiredValues) {
		if adapter.replaceable(candidate.EntryID()) {
			file.Conflicts = append(file.Conflicts, ConflictCandidate{
				Candidate: candidate,
				Resource:  resourceByID[ownedAggregateIDKey(candidate.EntryID())],
			})
		}
	}

	slog.Debug(repairLogMessage(key.target, "preview for aggregate"),
		"target", key.target, "machine_scope", key.machineScope, "file", fileSpec.rel,
		"existing_entries", len(state.existing), "desired_entries", len(desiredValues),
		"adoption_candidates", len(file.Adoptions), "orphan_candidates", len(file.Orphans),
		"conflict_candidates", len(file.Conflicts))
	return file, diagnostics
}

func readOwnedJSONRepairState(store *ownership.Store, key outputKey, outDir string, file ownedJSONFileSpec, options ownedJSONReconcileOptions) (*ownedJSONRepairState, []diag.Diagnostic) {
	rel := file.rel
	raw, err := ownership.ReadAggregateFile(filepath.Join(outDir, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []diag.Diagnostic{ownedAggregateDiag(options.writeCode, key, rel,
			fmt.Sprintf("read aggregate %q: %v", ownedAggregateLabel(outDir, rel), err))}
	}
	doc, err := jsonc.ParseWithOptions(rel, raw, options.parseOptions)
	if err != nil {
		return nil, []diag.Diagnostic{ownedAggregateCodedDiag(key, outDir, rel, err, options.malformedCode)}
	}
	container, containerMember, err := ownedJSONContainerNode(doc, file)
	if err != nil {
		return nil, []diag.Diagnostic{ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, err)}
	}
	state := &ownedJSONRepairState{raw: raw, doc: doc, container: container, containerMember: containerMember}
	if container != nil {
		state.existing, err = ownedJSONExistingEntries(file, container)
		if err != nil {
			return nil, []diag.Diagnostic{ownedJSONShapeDiag(options.shapeCode, key, outDir, rel, err)}
		}
	}
	state.existingValues = make([]ownership.EntryValue, 0, len(state.existing))
	for _, entry := range state.existing {
		state.existingValues = append(state.existingValues, entry.value)
	}
	proof, proofErr := store.Proof(rel)
	diagnostics := ownedAggregateProofDiags(key, outDir, rel, proofErr)
	state.proof = proof
	return state, diagnostics
}

func replaceOwnedJSONEntry(state *ownedJSONRepairState, rel string, id ownership.EntryID, raw []byte) error {
	if id.Key == "" {
		return fmt.Errorf("aggregate %q has no replaceable keyed entry for this identity", rel)
	}
	if _, ok := state.container.Member(id.Key); !ok {
		return fmt.Errorf("aggregate %q no longer has member %q", rel, id.Key)
	}
	if !state.doc.SetMember(state.container, id.Key, raw) {
		return fmt.Errorf("aggregate %q could not replace member %q", rel, id.Key)
	}
	return nil
}

func ownedJSONExistingEntryMatches(state *ownedJSONRepairState, id ownership.EntryID, valueDigest string) bool {
	want := ownedAggregateIDKey(id)
	for _, entry := range state.existing {
		if ownedAggregateIDKey(entry.value.ID) == want && entry.value.ValueDigest == valueDigest {
			return true
		}
	}
	return false
}

func removeOwnedJSONEntry(state *ownedJSONRepairState, file ownedJSONFileSpec, id ownership.EntryID, valueDigest string) error {
	if file.keyed {
		if !ownedJSONExistingEntryMatches(state, id, valueDigest) {
			return fmt.Errorf("aggregate %q member %q no longer holds the confirmed value", file.rel, id.Key)
		}
		if !state.doc.DeleteMember(state.container, id.Key) {
			return fmt.Errorf("aggregate %q could not delete member %q", file.rel, id.Key)
		}
		return nil
	}

	node, ok := state.container.Member(id.Container)
	if !ok || node.Kind() != jsonc.KindArray {
		return fmt.Errorf("aggregate %q has no %q hook array to edit", file.rel, id.Container)
	}
	kept := make([][]byte, 0, len(state.existing))
	removed := false
	for _, entry := range state.existing {
		if entry.container != id.Container {
			continue
		}
		if !removed && ownedAggregateIDKey(entry.value.ID) == ownedAggregateIDKey(id) && entry.value.ValueDigest == valueDigest {
			removed = true
			continue
		}
		kept = append(kept, entry.raw)
	}
	if !removed {
		return fmt.Errorf("aggregate %q hook entry for %q at the confirmed value was not found", file.rel, id.Container)
	}
	if len(kept) == 0 {
		if !state.doc.DeleteMember(state.container, id.Container) {
			return fmt.Errorf("aggregate %q could not delete hook event %q", file.rel, id.Container)
		}
		return nil
	}
	if !state.doc.SetElements(node, kept) {
		return fmt.Errorf("aggregate %q could not rewrite hook event %q", file.rel, id.Container)
	}
	return nil
}

func (a ownedJSONRepairAdapter) replaceable(id ownership.EntryID) bool {
	return a.file.keyed && id.Kind == ownership.KindMcpServer && id.Key != ""
}

func (a ownedJSONRepairAdapter) diagnosticSummary(diagnostics []diag.Diagnostic) string {
	if a.redact {
		return repairDiagnosticCodeSummary(diagnostics)
	}
	return repairDiagnosticSummary(diagnostics)
}

func repairDesiredFromOwnedJSON(entries []ownedJSONEntry) []repairDesiredEntry {
	desired := make([]repairDesiredEntry, 0, len(entries))
	for _, entry := range entries {
		desired = append(desired, repairDesiredEntry{
			value:    ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()},
			raw:      entry.raw,
			resource: entry.resource,
		})
	}
	return desired
}
