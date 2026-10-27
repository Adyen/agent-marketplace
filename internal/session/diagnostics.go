package session

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type DiagnosticSeverity uint8

const (
	DiagnosticError DiagnosticSeverity = iota
	DiagnosticWarning
)

type DiagnosticProvenance uint8

const (
	DiagnosticProvenanceSnapshot DiagnosticProvenance = iota
	DiagnosticProvenanceOperation
)

type DiagnosticCategory string

const (
	DiagnosticConfig          DiagnosticCategory = "config"
	DiagnosticCatalog         DiagnosticCategory = "catalog"
	DiagnosticResolution      DiagnosticCategory = "resolution"
	DiagnosticMaterialization DiagnosticCategory = "materialization"
	DiagnosticMutation        DiagnosticCategory = "mutation"
	DiagnosticSync            DiagnosticCategory = "sync"
	DiagnosticUpdate          DiagnosticCategory = "update"
	DiagnosticEditor          DiagnosticCategory = "editor"
)

// OperationKind names the guarded operation families diagnostics attribute
// to and the mutation serializer orders.
type OperationKind string

const (
	OperationMutation OperationKind = "mutation"
	OperationTargets  OperationKind = "targets"
	OperationSync     OperationKind = "sync"
	OperationUpdate   OperationKind = "update"
	OperationEditor   OperationKind = "editor"
)

// Diagnostic is one deduplicated issue observed during a snapshot read or a
// guarded operation.
type Diagnostic struct {
	Severity   DiagnosticSeverity
	Provenance DiagnosticProvenance
	Category   DiagnosticCategory
	Operation  OperationKind
	Attempt    uint64
	StableKey  string
	Err        error
}

// DiagnosticStore keeps snapshot diagnostics separate from operation
// diagnostics so each can be replaced and deduplicated independently.
type DiagnosticStore struct {
	Snapshot    []Diagnostic
	Operations  []Diagnostic
	nextAttempt uint64
}

func appendSnapshotDiagnostics(dst []Diagnostic, severity DiagnosticSeverity, category DiagnosticCategory, errs ...error) []Diagnostic {
	for _, err := range errs {
		if err != nil {
			dst = append(dst, SnapshotDiagnostic(severity, category, err))
		}
	}
	return dst
}

func SnapshotDiagnostic(severity DiagnosticSeverity, category DiagnosticCategory, err error) Diagnostic {
	return Diagnostic{
		Severity:   severity,
		Provenance: DiagnosticProvenanceSnapshot,
		Category:   category,
		StableKey:  stableDiagnosticKey(err),
		Err:        err,
	}
}

func stableDiagnosticKey(err error) string {
	return pipeline.IssueDedupKey(err)
}

func diagnosticCategoryFor(category pipeline.IssueCategory) DiagnosticCategory {
	switch category {
	case pipeline.IssueConfig:
		return DiagnosticConfig
	case pipeline.IssueCatalog:
		return DiagnosticCatalog
	case pipeline.IssueMaterialization:
		return DiagnosticMaterialization
	default:
		return DiagnosticResolution
	}
}

func diagnosticIdentity(d Diagnostic) string {
	identity := fmt.Sprint(d.Severity) + "\x00" + string(d.Category) + "\x00" + d.StableKey
	if d.Provenance == DiagnosticProvenanceOperation {
		identity = string(d.Operation) + "\x00" + fmt.Sprint(d.Attempt) + "\x00" + identity
	}
	return identity
}

func deduplicateDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	deduplicated := make([]Diagnostic, 0, len(diagnostics))
	seen := make(map[string]struct{}, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Err == nil {
			continue
		}
		key := diagnosticIdentity(diagnostic)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		deduplicated = append(deduplicated, diagnostic)
	}
	return deduplicated
}

func warningKeys(diagnostics []Diagnostic) map[string]struct{} {
	keys := make(map[string]struct{}, len(diagnostics))
	for _, d := range diagnostics {
		if d.Severity == DiagnosticWarning && d.Err != nil {
			keys[d.StableKey] = struct{}{}
		}
	}
	return keys
}

// retireOperationCopies drops operation warnings that repeated a warning of
// the previous snapshot which the accepted snapshot no longer reports.
func (s *DiagnosticStore) retireOperationCopies(previous, current map[string]struct{}) {
	kept := s.Operations[:0]
	for _, d := range s.Operations {
		if d.Severity == DiagnosticWarning {
			_, reported := previous[d.StableKey]
			_, still := current[d.StableKey]
			if reported && !still {
				continue
			}
		}
		kept = append(kept, d)
	}
	clear(s.Operations[len(kept):])
	s.Operations = kept
}

// Visible returns the snapshot diagnostics plus the operation diagnostics that
// do not repeat a current snapshot warning.
func (s DiagnosticStore) Visible() []Diagnostic {
	snapshotWarnings := warningKeys(s.Snapshot)
	visible := make([]Diagnostic, 0, len(s.Snapshot)+len(s.Operations))
	visible = append(visible, s.Snapshot...)
	for _, d := range s.Operations {
		if _, repeated := snapshotWarnings[d.StableKey]; repeated && d.Severity == DiagnosticWarning {
			continue
		}
		visible = append(visible, d)
	}
	return visible
}

func (s *DiagnosticStore) ReplaceSnapshot(diagnostics []Diagnostic) {
	diagnostics = append([]Diagnostic(nil), diagnostics...)
	previousWarnings := warningKeys(s.Snapshot)
	clear(s.Snapshot)
	for i := range diagnostics {
		diagnostics[i].Provenance = DiagnosticProvenanceSnapshot
		diagnostics[i].Operation = ""
		diagnostics[i].Attempt = 0
		if diagnostics[i].StableKey == "" {
			diagnostics[i].StableKey = stableDiagnosticKey(diagnostics[i].Err)
		}
	}
	s.Snapshot = deduplicateDiagnostics(diagnostics)
	s.retireOperationCopies(previousWarnings, warningKeys(s.Snapshot))
	slog.Debug("session snapshot diagnostics replaced", "count", len(s.Snapshot))
}

func (s *DiagnosticStore) RecordOperationFailure(operation OperationKind, category DiagnosticCategory, errs ...error) {
	s.nextAttempt++
	attempt := s.nextAttempt
	diagnostics := make([]Diagnostic, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   DiagnosticError,
			Provenance: DiagnosticProvenanceOperation,
			Category:   category,
			Operation:  operation,
			Attempt:    attempt,
			StableKey:  stableDiagnosticKey(err),
			Err:        err,
		})
	}
	diagnostics = deduplicateDiagnostics(diagnostics)
	originalLength := len(s.Operations)
	kept := s.Operations[:0]
	for _, diagnostic := range s.Operations {
		if diagnostic.Operation != operation {
			kept = append(kept, diagnostic)
		}
	}
	clear(s.Operations[len(kept):originalLength])
	s.Operations = append(kept, diagnostics...)
	slog.Error("session operation failed", "operation", operation, "category", category, "attempt", attempt, "error_count", len(diagnostics))
}

func (s *DiagnosticStore) RecordOperationWarnings(operation OperationKind, category DiagnosticCategory, errs ...error) {
	s.nextAttempt++
	attempt := s.nextAttempt
	recorded := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		s.Operations = append(s.Operations, Diagnostic{
			Severity:   DiagnosticWarning,
			Provenance: DiagnosticProvenanceOperation,
			Category:   category,
			Operation:  operation,
			Attempt:    attempt,
			StableKey:  stableDiagnosticKey(err),
			Err:        err,
		})
		recorded++
	}
	s.Operations = deduplicateDiagnostics(s.Operations)
	slog.Warn("session operation completed with warnings", "operation", operation, "category", category, "attempt", attempt, "warning_count", recorded)
}

func (s *DiagnosticStore) RecordOperationSuccess(operation OperationKind) {
	originalLength := len(s.Operations)
	kept := s.Operations[:0]
	cleared := 0
	for _, diagnostic := range s.Operations {
		if diagnostic.Operation == operation {
			cleared++
			continue
		}
		kept = append(kept, diagnostic)
	}
	clear(s.Operations[len(kept):originalLength])
	s.Operations = kept
	if cleared > 0 {
		slog.Info("session operation recovered", "operation", operation, "cleared_diagnostics", cleared)
	}
}

func (s DiagnosticStore) All() []Diagnostic {
	all := make([]Diagnostic, 0, len(s.Snapshot)+len(s.Operations))
	all = append(all, s.Snapshot...)
	all = append(all, s.Operations...)
	return all
}

func DiagnosticErrors(diagnostics []Diagnostic) []error {
	return DiagnosticValues(diagnostics, DiagnosticError)
}

func DiagnosticsFromLegacy(errs, warnings []error) []Diagnostic {
	diagnostics := appendSnapshotDiagnostics(nil, DiagnosticError, DiagnosticConfig, errs...)
	return appendSnapshotDiagnostics(diagnostics, DiagnosticWarning, DiagnosticConfig, warnings...)
}

func DiagnosticValues(diagnostics []Diagnostic, severity DiagnosticSeverity) []error {
	values := make([]error, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity && diagnostic.Err != nil {
			values = append(values, diagnostic.Err)
		}
	}
	return values
}
