package tui

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type diagnosticSeverity uint8

const (
	diagnosticError diagnosticSeverity = iota
	diagnosticWarning
)

type diagnosticProvenance uint8

const (
	diagnosticSnapshot diagnosticProvenance = iota
	diagnosticOperation
)

type diagnosticCategory string

const (
	diagnosticConfig          diagnosticCategory = "config"
	diagnosticCatalog         diagnosticCategory = "catalog"
	diagnosticResolution      diagnosticCategory = "resolution"
	diagnosticMaterialization diagnosticCategory = "materialization"
	diagnosticMutation        diagnosticCategory = "mutation"
	diagnosticSync            diagnosticCategory = "sync"
	diagnosticUpdate          diagnosticCategory = "update"
	diagnosticEditor          diagnosticCategory = "editor"
)

type operationKind string

const (
	operationMutation   operationKind = "mutation"
	operationTargets    operationKind = "targets"
	operationSync       operationKind = "sync"
	operationUpdate     operationKind = "update"
	operationEditor     operationKind = "editor"
	operationHookReview operationKind = "hook-review"
)

type tuiDiagnostic struct {
	severity   diagnosticSeverity
	provenance diagnosticProvenance
	category   diagnosticCategory
	operation  operationKind
	attempt    uint64
	stableKey  string
	err        error
}

type diagnosticStore struct {
	snapshot    []tuiDiagnostic
	operations  []tuiDiagnostic
	nextAttempt uint64
}

func appendSnapshotDiagnostics(dst []tuiDiagnostic, severity diagnosticSeverity, category diagnosticCategory, errs ...error) []tuiDiagnostic {
	for _, err := range errs {
		if err != nil {
			dst = append(dst, snapshotDiagnostic(severity, category, err))
		}
	}
	return dst
}

func snapshotDiagnostic(severity diagnosticSeverity, category diagnosticCategory, err error) tuiDiagnostic {
	return tuiDiagnostic{
		severity:   severity,
		provenance: diagnosticSnapshot,
		category:   category,
		stableKey:  stableDiagnosticKey(err),
		err:        err,
	}
}

func stableDiagnosticKey(err error) string {
	return pipeline.IssueDedupKey(err)
}

func diagnosticCategoryFor(category pipeline.IssueCategory) diagnosticCategory {
	switch category {
	case pipeline.IssueConfig:
		return diagnosticConfig
	case pipeline.IssueCatalog:
		return diagnosticCatalog
	case pipeline.IssueMaterialization:
		return diagnosticMaterialization
	default:
		return diagnosticResolution
	}
}

func diagnosticIdentity(d tuiDiagnostic) string {
	identity := fmt.Sprint(d.severity) + "\x00" + string(d.category) + "\x00" + d.stableKey
	if d.provenance == diagnosticOperation {
		identity = string(d.operation) + "\x00" + fmt.Sprint(d.attempt) + "\x00" + identity
	}
	return identity
}

func deduplicateDiagnostics(diagnostics []tuiDiagnostic) []tuiDiagnostic {
	deduplicated := make([]tuiDiagnostic, 0, len(diagnostics))
	seen := make(map[string]struct{}, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.err == nil {
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

func (s *diagnosticStore) replaceSnapshot(diagnostics []tuiDiagnostic) {
	previousWarnings := warningKeys(s.snapshot)
	diagnostics = append([]tuiDiagnostic(nil), diagnostics...)
	clear(s.snapshot)
	for i := range diagnostics {
		diagnostics[i].provenance = diagnosticSnapshot
		diagnostics[i].operation = ""
		diagnostics[i].attempt = 0
		if diagnostics[i].stableKey == "" {
			diagnostics[i].stableKey = stableDiagnosticKey(diagnostics[i].err)
		}
	}
	s.snapshot = deduplicateDiagnostics(diagnostics)
	s.retireOperationCopies(previousWarnings, warningKeys(s.snapshot))
	slog.Debug("tui snapshot diagnostics replaced", "count", len(s.snapshot))
}

// warningKeys returns the stable keys of the warnings among diagnostics.
func warningKeys(diagnostics []tuiDiagnostic) map[string]struct{} {
	keys := make(map[string]struct{}, len(diagnostics))
	for _, d := range diagnostics {
		if d.severity == diagnosticWarning && d.err != nil {
			keys[d.stableKey] = struct{}{}
		}
	}
	return keys
}

// retireOperationCopies drops operation warnings that repeated a warning of
// the previous snapshot which the accepted snapshot no longer reports: both
// described the same configuration state, which has been resolved.
func (s *diagnosticStore) retireOperationCopies(previous, current map[string]struct{}) {
	if len(previous) == 0 {
		return
	}
	originalLength := len(s.operations)
	kept := s.operations[:0]
	retired := 0
	for _, d := range s.operations {
		if d.severity == diagnosticWarning {
			_, reported := previous[d.stableKey]
			_, still := current[d.stableKey]
			if reported && !still {
				retired++
				continue
			}
		}
		kept = append(kept, d)
	}
	clear(s.operations[len(kept):originalLength])
	s.operations = kept
	if retired > 0 {
		slog.Debug("tui operation warnings retired by snapshot", "retired", retired)
	}
}

func (s *diagnosticStore) recordOperationFailure(operation operationKind, category diagnosticCategory, errs ...error) {
	s.nextAttempt++
	attempt := s.nextAttempt
	diagnostics := make([]tuiDiagnostic, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		diagnostics = append(diagnostics, tuiDiagnostic{
			severity:   diagnosticError,
			provenance: diagnosticOperation,
			category:   category,
			operation:  operation,
			attempt:    attempt,
			stableKey:  stableDiagnosticKey(err),
			err:        err,
		})
	}
	diagnostics = deduplicateDiagnostics(diagnostics)
	originalLength := len(s.operations)
	kept := s.operations[:0]
	for _, diagnostic := range s.operations {
		if diagnostic.operation != operation {
			kept = append(kept, diagnostic)
		}
	}
	clear(s.operations[len(kept):originalLength])
	s.operations = append(kept, diagnostics...)
	slog.Error("tui operation failed", "operation", operation, "category", category, "attempt", attempt, "error_count", len(diagnostics))
}

func (s *diagnosticStore) recordOperationWarnings(operation operationKind, category diagnosticCategory, errs ...error) {
	s.nextAttempt++
	attempt := s.nextAttempt
	recorded := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		s.operations = append(s.operations, tuiDiagnostic{
			severity:   diagnosticWarning,
			provenance: diagnosticOperation,
			category:   category,
			operation:  operation,
			attempt:    attempt,
			stableKey:  stableDiagnosticKey(err),
			err:        err,
		})
		recorded++
	}
	s.operations = deduplicateDiagnostics(s.operations)
	slog.Warn("tui operation completed with warnings", "operation", operation, "category", category, "attempt", attempt, "warning_count", recorded)
}

func (s *diagnosticStore) recordOperationSuccess(operation operationKind) {
	originalLength := len(s.operations)
	kept := s.operations[:0]
	cleared := 0
	for _, diagnostic := range s.operations {
		if diagnostic.operation == operation {
			cleared++
			continue
		}
		kept = append(kept, diagnostic)
	}
	clear(s.operations[len(kept):originalLength])
	s.operations = kept
	if cleared > 0 {
		slog.Info("tui operation recovered", "operation", operation, "cleared_diagnostics", cleared)
	}
}

// visible returns the diagnostics the user can still act on: an operation
// warning that repeats a current snapshot warning is hidden, because the
// snapshot copy already reports the same state.
func (s diagnosticStore) visible() []tuiDiagnostic {
	snapshotWarnings := warningKeys(s.snapshot)
	visible := make([]tuiDiagnostic, 0, len(s.snapshot)+len(s.operations))
	visible = append(visible, s.snapshot...)
	for _, d := range s.operations {
		if _, repeated := snapshotWarnings[d.stableKey]; repeated && d.severity == diagnosticWarning {
			continue
		}
		visible = append(visible, d)
	}
	return visible
}

func (s diagnosticStore) all() []tuiDiagnostic {
	all := make([]tuiDiagnostic, 0, len(s.snapshot)+len(s.operations))
	all = append(all, s.snapshot...)
	all = append(all, s.operations...)
	return all
}

func diagnosticErrors(diagnostics []tuiDiagnostic) []error {
	return diagnosticValues(diagnostics, diagnosticError)
}

func diagnosticsFromLegacy(errs, warnings []error) []tuiDiagnostic {
	diagnostics := appendSnapshotDiagnostics(nil, diagnosticError, diagnosticConfig, errs...)
	return appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticConfig, warnings...)
}

func diagnosticValues(diagnostics []tuiDiagnostic, severity diagnosticSeverity) []error {
	values := make([]error, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.severity == severity && diagnostic.err != nil {
			values = append(values, diagnostic.err)
		}
	}
	return values
}

func (m *Model) syncDiagnosticViews() {
	visible := m.diagnostics.visible()
	m.errs = diagnosticValues(visible, diagnosticError)
	m.warnings = diagnosticValues(visible, diagnosticWarning)
}

func (m *Model) replaceSnapshotDiagnostics(diagnostics []tuiDiagnostic, errs, warnings []error) {
	if len(diagnostics) == 0 {
		diagnostics = diagnosticsFromLegacy(errs, warnings)
	}
	m.diagnostics.replaceSnapshot(diagnostics)
	m.syncDiagnosticViews()
}

func (m *Model) seedLegacyDiagnostics() {
	if len(m.diagnostics.snapshot) == 0 && len(m.diagnostics.operations) == 0 && (len(m.errs) > 0 || len(m.warnings) > 0) {
		m.diagnostics.replaceSnapshot(diagnosticsFromLegacy(m.errs, m.warnings))
	}
}

func (m *Model) recordOperationFailure(operation operationKind, category diagnosticCategory, errs ...error) {
	m.seedLegacyDiagnostics()
	m.diagnostics.recordOperationFailure(operation, category, errs...)
	m.syncDiagnosticViews()
}

func (m *Model) recordOperationSuccess(operation operationKind) {
	m.seedLegacyDiagnostics()
	m.diagnostics.recordOperationSuccess(operation)
	m.syncDiagnosticViews()
}

func (m *Model) recordOperationWarnings(operation operationKind, category diagnosticCategory, errs ...error) {
	m.seedLegacyDiagnostics()
	var ordinary, review []error
	for _, err := range errs {
		if isHookReviewWarning(err) {
			review = append(review, err)
		} else {
			ordinary = append(ordinary, err)
		}
	}
	if len(ordinary) > 0 {
		m.diagnostics.recordOperationWarnings(operation, category, ordinary...)
	}
	if len(review) > 0 {
		m.diagnostics.recordOperationWarnings(operationHookReview, diagnosticMaterialization, review...)
	}
	m.syncDiagnosticViews()
}
