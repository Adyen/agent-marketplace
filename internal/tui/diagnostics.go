package tui

import (
	"errors"
	"fmt"
	"log/slog"
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
	diagnosticMigration       diagnosticCategory = "migration"
	diagnosticEditor          diagnosticCategory = "editor"
)

type operationKind string

const (
	operationMutation  operationKind = "mutation"
	operationTargets   operationKind = "targets"
	operationSync      operationKind = "sync"
	operationUpdate    operationKind = "update"
	operationMigration operationKind = "migration"
	operationEditor    operationKind = "editor"
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
	if err == nil {
		return "<nil>"
	}
	type keyer interface{ DedupKey() string }
	var keyed keyer
	if errors.As(err, &keyed) {
		if key := keyed.DedupKey(); key != "" {
			return key
		}
	}
	return fmt.Sprintf("%T\x00%s", err, err.Error())
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
	slog.Debug("tui snapshot diagnostics replaced", "count", len(s.snapshot))
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
	m.errs = diagnosticValues(m.diagnostics.all(), diagnosticError)
	m.warnings = diagnosticValues(m.diagnostics.all(), diagnosticWarning)
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
