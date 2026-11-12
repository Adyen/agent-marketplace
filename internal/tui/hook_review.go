package tui

import (
	"errors"
	"slices"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func (m *Model) setHookReview(report *materialize.Report) {
	if report == nil {
		return
	}
	m.seedLegacyDiagnostics()
	m.hookChanges = slices.Clone(report.HookChanges)
	m.diagnostics.RecordOperationSuccess(operationHookReview)
	m.syncDiagnosticViews()
}

func isHookReviewWarning(err error) bool {
	var coded interface{ DiagnosticCode() string }
	return errors.As(err, &coded) && (coded.DiagnosticCode() == diag.CodeMaterializeHookReview || coded.DiagnosticCode() == diag.CodeMaterializeCodexTrustReview)
}

func (m *Model) presentHookReview() {
	if len(m.hookChanges) > 0 {
		m.openMaterializationReview()
	}
}

func (m *Model) openMaterializationReview() {
	m.pane = paneErrors
	m.errorsViewport.SetContent(renderErrors(*m))
	m.errorsViewport.GotoTop()
}

func (m *Model) finishMutationReview() {
	report, warnings := m.mutationPendingReport, m.mutationPendingWarnings
	m.mutationPendingReport, m.mutationPendingWarnings = nil, nil
	if report != nil {
		m.setHookReview(report)
	}
	if len(warnings) > 0 {
		m.recordOperationWarnings(operationMutation, diagnosticMutation, warnings...)
	}
	if report != nil {
		m.presentHookReview()
	}
}
