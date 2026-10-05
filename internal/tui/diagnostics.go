package tui

// The diagnostic store and its pure helpers live in internal/session; this
// file keeps the Model-facing adapters that also refresh the error-pane
// views.

func (m *Model) syncDiagnosticViews() {
	m.errs = diagnosticValues(m.diagnostics.All(), diagnosticError)
	m.warnings = diagnosticValues(m.diagnostics.All(), diagnosticWarning)
}

func (m *Model) replaceSnapshotDiagnostics(diagnostics []tuiDiagnostic, errs, warnings []error) {
	if len(diagnostics) == 0 {
		diagnostics = diagnosticsFromLegacy(errs, warnings)
	}
	m.diagnostics.ReplaceSnapshot(diagnostics)
	m.syncDiagnosticViews()
}

func (m *Model) seedLegacyDiagnostics() {
	if len(m.diagnostics.Snapshot) == 0 && len(m.diagnostics.Operations) == 0 && (len(m.errs) > 0 || len(m.warnings) > 0) {
		m.diagnostics.ReplaceSnapshot(diagnosticsFromLegacy(m.errs, m.warnings))
	}
}

func (m *Model) recordOperationFailure(operation operationKind, category diagnosticCategory, errs ...error) {
	m.seedLegacyDiagnostics()
	m.diagnostics.RecordOperationFailure(operation, category, errs...)
	m.syncDiagnosticViews()
}

func (m *Model) recordOperationSuccess(operation operationKind) {
	m.seedLegacyDiagnostics()
	m.diagnostics.RecordOperationSuccess(operation)
	m.syncDiagnosticViews()
}

func (m *Model) recordOperationWarnings(operation operationKind, category diagnosticCategory, errs ...error) {
	m.seedLegacyDiagnostics()
	m.diagnostics.RecordOperationWarnings(operation, category, errs...)
	m.syncDiagnosticViews()
}
