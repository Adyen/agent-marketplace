package materialize

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const codexRepairMalformedCode = "materialize.codex.aggregate-malformed"

// CodexRepairPreview computes the read-only repair picture for Codex hooks and
// MCP configuration in every selected repository or machine Codex root.
func CodexRepairPreview(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) (*RepairPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.Info("codex repair preview start", "repo_root", repoRoot)
	preview := &RepairPreview{}
	compiled, keys := compileRepairTarget(repoRoot, resolved, hcc.Target(targetmeta.TargetCodex))
	preview.Diags = append(preview.Diags, redactCodexRepairDiags(repoRoot, compiled.allDiags)...)
	registry, err := harness.NewInstallationAdapterRegistry()
	if err != nil {
		return nil, fmt.Errorf("construct Codex repair adapter: %w", err)
	}
	adapter, ok := registry.Lookup(hcc.Target(targetmeta.TargetCodex))
	if !ok {
		return nil, fmt.Errorf("codex repair adapter is not registered")
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		home := ""
		if key.machineScope {
			home, err = os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("resolve Codex repair home directory: %w", err)
			}
		}
		destinations, err := adapter.Resolve(key.machineScope, repoRoot, home)
		if err != nil {
			return nil, fmt.Errorf("resolve Codex repair destinations: %w", err)
		}
		if len(destinations) == 0 {
			return nil, fmt.Errorf("codex repair adapter returned no destinations")
		}
		for _, destination := range destinations {
			if !destination.Info.OwnershipReconciledAggregates {
				continue
			}
			state, _ := compiled.ownedState[key].(*codexOwnedAggregateState)
			if state == nil {
				state = &codexOwnedAggregateState{}
			}
			previewCodexRoot(preview, key, destination.Root, state.hooks, state.mcps)
		}
	}

	slog.Info("codex repair preview complete", "repo_root", repoRoot, "files", len(preview.Files), "diagnostics", len(preview.Diags))
	return preview, nil
}

func previewCodexRoot(preview *RepairPreview, key outputKey, root string, hooks *ownedJSONAggregateWork, mcps []codexMCPEntry) {
	var store *ownership.Store
	for _, rel := range []string{codexHooksFile, codexMCPFile} {
		var desiredHooks []ownedJSONEntry
		hasDesired := len(mcps) > 0
		if rel == codexHooksFile {
			hasDesired = false
			if hooks != nil {
				for _, entry := range hooks.entries {
					if entry.rel == rel {
						desiredHooks = append(desiredHooks, entry)
						hasDesired = true
					}
				}
			}
		}
		abs := filepath.Join(root, rel)
		exists := true
		if _, err := os.Lstat(abs); err != nil {
			exists = false
			if !os.IsNotExist(err) {
				diagnostic := ownedAggregateCodedDiag(key, root, rel, err, codexRepairMalformedCode)
				preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, []diag.Diagnostic{diagnostic})...)
				continue
			}
			if !hasDesired {
				continue
			}
		}
		if store == nil {
			opened, err := ownership.Open(root, ownership.WithLockObserver(observeRepairLock))
			if err != nil {
				diagnostic := ownedAggregateCodedDiag(key, root, "", err, codexRepairMalformedCode)
				preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, []diag.Diagnostic{diagnostic})...)
				return
			}
			store = opened
		}
		if !exists {
			_, proofErr := store.Proof(rel)
			preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, ownedAggregateProofDiags(key, root, rel, proofErr))...)
			continue
		}
		var file *RepairFile
		var fileDiags []diag.Diagnostic
		switch rel {
		case codexHooksFile:
			hookFile := ownedJSONFileSpec{rel: codexHooksFile, container: codexHooksContainer, rawValueDigest: true}
			file, fileDiags = previewOwnedJSONAggregateFile(store, key, root, hookFile, codexHookJSONOptions(), true, desiredHooks)
		case codexMCPFile:
			file, fileDiags = previewCodexMCPFile(store, key, root, mcps)
		}
		preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, fileDiags)...)
		if file != nil {
			preview.Files = append(preview.Files, file)
		}
	}
}

func previewCodexMCPFile(store *ownership.Store, key outputKey, root string, desired []codexMCPEntry) (*RepairFile, []diag.Diagnostic) {
	rel := codexMCPFile
	adapter := codexMCPRepairAdapter{}
	nativeState, diagnostics := readCodexMCPRepairState(store, key, root)
	if nativeState == nil {
		return nil, diagnostics
	}
	state := codexMCPAggregateState(nativeState)
	desiredEntries := repairDesiredFromCodexMCP(desired)
	desiredValues := repairDesiredValues(desiredEntries)
	resourceByID := make(map[string]string, len(desired))
	for _, entry := range desired {
		resourceByID[ownedAggregateIDKey(entry.id())] = entry.resource
	}
	file := &RepairFile{
		Target:          key.target,
		MachineScope:    key.machineScope,
		OutDir:          root,
		Rel:             rel,
		ExistingEntries: len(state.existing),
		DesiredEntries:  len(desiredValues),
		store:           store,
		adapter:         adapter,
		desired:         desiredEntries,
	}
	for _, candidate := range ownership.ExactAdoptionCandidates(state.proof, state.existing, desiredValues) {
		file.Adoptions = append(file.Adoptions, AdoptionCandidate{Candidate: candidate, Resource: resourceByID[ownedAggregateIDKey(candidate.EntryID())]})
	}
	orphans, orphanErr := store.OrphanCandidates(state.proof, rel, state.existing, desiredValues)
	if orphanErr != nil {
		diagnostics = append(diagnostics, ownedAggregateCodedDiag(key, root, rel, orphanErr, codexRepairMalformedCode))
	}
	for _, candidate := range orphans {
		section, ok := nativeState.sections[candidate.EntryID().Key]
		if !ok || section.alternate {
			continue
		}
		file.Orphans = append(file.Orphans, OrphanCandidate{Candidate: candidate})
	}
	for _, candidate := range ownership.ValueConflictCandidates(state.proof, state.existing, desiredValues) {
		section, ok := nativeState.sections[candidate.EntryID().Key]
		if !ok || section.alternate {
			continue
		}
		file.Conflicts = append(file.Conflicts, ConflictCandidate{
			Candidate: candidate,
			Resource:  resourceByID[ownedAggregateIDKey(candidate.EntryID())],
		})
	}
	slog.Debug("codex repair preview for aggregate",
		"target", key.target, "machine_scope", key.machineScope, "file", rel,
		"existing_entries", len(state.existing), "desired_entries", len(desiredValues),
		"adoption_candidates", len(file.Adoptions), "orphan_candidates", len(file.Orphans),
		"conflict_candidates", len(file.Conflicts))
	return file, diagnostics
}

type codexMCPRepairState struct {
	raw      []byte
	sections map[string]codexMCPSection
	existing []ownership.EntryValue
	proof    ownership.Proof
}

type codexMCPRepairAdapter struct{}

func (codexMCPRepairAdapter) read(store *ownership.Store, key outputKey, root, _ string) (*repairAggregateState, []diag.Diagnostic) {
	state, diagnostics := readCodexMCPRepairState(store, key, root)
	if state == nil {
		return nil, diagnostics
	}
	return codexMCPAggregateState(state), diagnostics
}

func (codexMCPRepairAdapter) replaceable(id ownership.EntryID) bool {
	return id.Kind == ownership.KindMcpServer && id.Key != ""
}

func (codexMCPRepairAdapter) diagnosticSummary(diagnostics []diag.Diagnostic) string {
	return repairDiagnosticCodeSummary(diagnostics)
}

func codexMCPAggregateState(state *codexMCPRepairState) *repairAggregateState {
	return &repairAggregateState{
		raw:      state.raw,
		existing: state.existing,
		proof:    state.proof,
		remove:   state.remove,
		replace:  state.replace,
	}
}

func repairDesiredFromCodexMCP(entries []codexMCPEntry) []repairDesiredEntry {
	desired := make([]repairDesiredEntry, 0, len(entries))
	for _, entry := range entries {
		desired = append(desired, repairDesiredEntry{
			value:    ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest},
			raw:      entry.raw,
			resource: entry.resource,
		})
	}
	return desired
}

func (s *codexMCPRepairState) remove(id ownership.EntryID, valueDigest string) ([]byte, error) {
	section, ok := s.sections[id.Key]
	if !ok || section.value.ValueDigest != valueDigest {
		return nil, fmt.Errorf("codex MCP server %q no longer holds the confirmed value", id.Key)
	}
	if section.alternate {
		return nil, fmt.Errorf("codex MCP server %q uses an alternate TOML form that cannot be removed safely", id.Key)
	}
	next := make([]byte, 0, len(s.raw)-(section.end-section.start))
	next = append(next, s.raw[:section.start]...)
	next = append(next, s.raw[section.end:]...)
	return next, nil
}

func (s *codexMCPRepairState) replace(id ownership.EntryID, replacement []byte) ([]byte, error) {
	section, ok := s.sections[id.Key]
	if !ok {
		return nil, fmt.Errorf("codex MCP server %q no longer exists", id.Key)
	}
	if section.alternate {
		return nil, fmt.Errorf("codex MCP server %q uses an alternate TOML form that cannot be replaced safely", id.Key)
	}
	next := make([]byte, 0, len(s.raw)-(section.end-section.start)+len(replacement))
	next = append(next, s.raw[:section.start]...)
	next = append(next, replacement...)
	next = append(next, s.raw[section.end:]...)
	return next, nil
}

func readCodexMCPRepairState(store *ownership.Store, key outputKey, root string) (*codexMCPRepairState, []diag.Diagnostic) {
	rel := codexMCPFile
	raw, err := store.ReadAggregate(rel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []diag.Diagnostic{ownedAggregateCodedDiag(key, root, rel, err, codexRepairMalformedCode)}
	}
	sections, existing, err := codexMCPSections(raw)
	if err != nil {
		return nil, []diag.Diagnostic{codexRepairFileDiag(key, root, rel, codexRepairMalformedCode)}
	}
	proof, proofErr := store.Proof(rel)
	return &codexMCPRepairState{raw: raw, sections: sections, existing: existing, proof: proof}, redactCodexRepairDiags(root, ownedAggregateProofDiags(key, root, rel, proofErr))
}

func redactCodexRepairDiags(root string, diagnostics []diag.Diagnostic) []diag.Diagnostic {
	redacted := make([]diag.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Target != "" && diagnostic.Target != string(hcc.TargetCodex) {
			redacted = append(redacted, diagnostic)
			continue
		}
		rel := diagnostic.Source.File
		if filepath.IsAbs(rel) {
			rel = ""
		}
		message := "Codex shared configuration is malformed or unsupported; it was left unchanged"
		if strings.HasPrefix(diagnostic.Code, "ownership.") {
			message = "Codex ownership state is missing, corrupt, or belongs to another root; no repair was attempted"
		}
		diagnostic.Message = ownedAggregateMessage(root, rel, message)
		redacted = append(redacted, diagnostic)
	}
	return redacted
}

func codexRepairFileDiag(key outputKey, root, rel, code string) diag.Diagnostic {
	return ownedAggregateDiag(code, key, rel, ownedAggregateMessage(root, rel,
		"Codex shared configuration is malformed or unsupported; it was left unchanged"))
}
