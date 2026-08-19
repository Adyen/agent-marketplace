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

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
		if err != nil {
			preview.Diags = append(preview.Diags, diag.Diagnostic{
				Code:     "materialize.destination.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("resolve Codex repair destination: %v", err),
			})
			continue
		}
		for _, destination := range destinations {
			if destination.Prefix != ".codex" {
				continue
			}
			previewCodexRoot(preview, key, destination.Root, compiled.codexAggregates[key], compiled.codexMCP[key])
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
				diagnostic := devinAggregateCodedDiag(key, root, rel, err, codexRepairMalformedCode)
				preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, []diag.Diagnostic{diagnostic})...)
				continue
			}
			if !hasDesired {
				continue
			}
		}
		if store == nil {
			opened, err := ownership.Open(root, ownership.WithLockObserver(observeDevinRepairLock))
			if err != nil {
				diagnostic := devinAggregateCodedDiag(key, root, "", err, codexRepairMalformedCode)
				preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, []diag.Diagnostic{diagnostic})...)
				return
			}
			store = opened
		}
		if !exists {
			_, proofErr := store.Proof(rel)
			preview.Diags = append(preview.Diags, redactCodexRepairDiags(root, devinProofDiags(key, root, rel, proofErr))...)
			continue
		}
		var file *RepairFile
		var fileDiags []diag.Diagnostic
		switch rel {
		case codexHooksFile:
			file, fileDiags = previewDevinAggregateFile(store, key, root, rel, desiredHooks)
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
	state, diagnostics := readCodexMCPRepairState(store, key, root)
	if state == nil {
		return nil, diagnostics
	}
	desiredValues := make([]ownership.EntryValue, 0, len(desired))
	resourceByID := make(map[string]string, len(desired))
	for _, entry := range desired {
		value := ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest}
		desiredValues = append(desiredValues, value)
		resourceByID[devinIDKey(value.ID)] = entry.resource
	}
	file := &RepairFile{
		Target:          key.target,
		MachineScope:    key.machineScope,
		OutDir:          root,
		Rel:             rel,
		ExistingEntries: len(state.existing),
		DesiredEntries:  len(desiredValues),
		store:           store,
		codexMCPDesired: append([]codexMCPEntry(nil), desired...),
	}
	for _, candidate := range ownership.ExactAdoptionCandidates(state.proof, state.existing, desiredValues) {
		file.Adoptions = append(file.Adoptions, AdoptionCandidate{Candidate: candidate, Resource: resourceByID[devinIDKey(candidate.EntryID())]})
	}
	orphans, orphanErr := store.OrphanCandidates(state.proof, rel, state.existing, desiredValues)
	if orphanErr != nil {
		diagnostics = append(diagnostics, devinAggregateCodedDiag(key, root, rel, orphanErr, codexRepairMalformedCode))
	}
	for _, candidate := range orphans {
		section, ok := state.sections[candidate.EntryID().Key]
		if !ok || section.alternate {
			continue
		}
		file.Orphans = append(file.Orphans, OrphanCandidate{Candidate: candidate})
	}
	for _, candidate := range ownership.ValueConflictCandidates(state.proof, state.existing, desiredValues) {
		section, ok := state.sections[candidate.EntryID().Key]
		if !ok || section.alternate {
			continue
		}
		file.Conflicts = append(file.Conflicts, ConflictCandidate{
			Candidate: candidate,
			Resource:  resourceByID[devinIDKey(candidate.EntryID())],
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
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []diag.Diagnostic{devinAggregateCodedDiag(key, root, rel, err, codexRepairMalformedCode)}
	}
	sections, existing, err := codexMCPSections(raw)
	if err != nil {
		return nil, []diag.Diagnostic{codexRepairFileDiag(key, root, rel, codexRepairMalformedCode)}
	}
	proof, proofErr := store.Proof(rel)
	return &codexMCPRepairState{raw: raw, sections: sections, existing: existing, proof: proof}, redactCodexRepairDiags(root, devinProofDiags(key, root, rel, proofErr))
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
		diagnostic.Message = devinAggregateMessage(root, rel, message)
		redacted = append(redacted, diagnostic)
	}
	return redacted
}

func codexRepairFileDiag(key outputKey, root, rel, code string) diag.Diagnostic {
	return devinDiag(code, key, rel, devinAggregateMessage(root, rel,
		"Codex shared configuration is malformed or unsupported; it was left unchanged"))
}
