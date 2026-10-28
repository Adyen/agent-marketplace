package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: hook-sync-diagnostics/tui-presents-and-retains-completed-hook-review
// spec: hook-sync-diagnostics/sync-and-quit-presents-new-hook-review
func TestCompletedHookReviewSurvivesTUIReload(t *testing.T) {
	for _, operation := range []string{"sync", "update", "marketplace", "toggle", "sync-quit", "sync-quit-failed"} {
		t.Run(operation, func(t *testing.T) {
			report := &materialize.Report{HookChanges: []materialize.HookChange{{
				Target: "claude", File: "/repo/.claude/settings.json", Event: "PreToolUse", Matcher: "Bash",
				After: json.RawMessage(`{"hooks":[{"type":"command","command":"echo review\nvalue\u001b[2J"}]}`),
			}}}
			warning := pipeline.PromoteDiagnostic(diag.Diagnostic{Code: diag.CodeMaterializeHookReview, Severity: diag.SeverityWarning, Message: "review changed hooks"})
			failure := errors.New("independent materialization failed")
			previousSync, previousSnapshot := session.RunPipelineSync, readSnapshotForUpdate
			session.RunPipelineSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
				result := pipeline.SyncResult{Report: report}
				if len(report.HookChanges) > 0 {
					result.Warnings = []error{warning}
					if operation == "sync-quit-failed" {
						result.Errors = []error{failure}
					}
				}
				return result
			}
			readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
				return snapshotLoadedMsg{mergedCfg: &config.Config{}, driftAssessment: driftReady}
			}
			t.Cleanup(func() { session.RunPipelineSync, readSnapshotForUpdate = previousSync, previousSnapshot })
			model := NewModel(t.TempDir(), t.TempDir())
			model.loading = false
			model.syncing = true
			model.driftAssessment = driftReady
			var message tea.Msg
			switch operation {
			case "update":
				message = model.dependencyUpdateCmd(model.beginRequest(requestSnapshot))()
			case "marketplace":
				id, ok := model.beginOperation(operationMutation)
				if !ok {
					t.Fatal("install operation not accepted")
				}
				model.marketplaceInstallOperation = id
				message = discoveryInstallMsg{operation: id, result: &pipeline.InstallResult{Report: report, RequirementWarnings: []error{warning}}}
			case "toggle":
				id, ok := model.beginOperation(operationMutation)
				if !ok {
					t.Fatal("toggle operation not accepted")
				}
				message = configWrittenMsg{operation: id, report: report, warnings: []error{warning}}
			default:
				model.quitAfterSync = strings.HasPrefix(operation, "sync-quit")
				message = model.dependencySyncCmd()()
			}
			updated, _ := model.Update(message)
			model = updated.(Model)
			updated, _ = model.Update(snapshotLoadedMsg{generation: model.requests.Active(requestSnapshot), mergedCfg: &config.Config{}, driftAssessment: driftReady})
			model = updated.(Model)
			if model.pane != paneErrors {
				t.Fatalf("%s left review hidden in pane %v", operation, model.pane)
			}
			if operation == "sync-quit-failed" && !errors.Is(model.syncQuitErr, failure) {
				t.Fatal("review changed the failed sync-and-quit exit status")
			}
			assertReview := func() {
				t.Helper()
				output := renderErrors(model)
				for _, want := range []string{"Hook changes:", "claude (repository) add", "echo review", "materialize.hook.review"} {
					if !strings.Contains(output, want) {
						t.Fatalf("review missing %q:\n%s", want, output)
					}
				}
				if strings.Contains(output, "\x1b[2J") {
					t.Fatal("review emitted a terminal control sequence")
				}
			}
			assertReview()
			updated, _ = model.Update(snapshotLoadedMsg{generation: model.beginRequest(requestSnapshot), mergedCfg: &config.Config{}, driftAssessment: driftReady})
			model = updated.(Model)
			assertReview()
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
			model = updated.(Model)
			assertReview()
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			model = updated.(Model)
			report = &materialize.Report{}
			model.syncing = true
			updated, _ = model.Update(model.dependencySyncCmd()())
			model = updated.(Model)
			updated, _ = model.Update(snapshotLoadedMsg{generation: model.requests.Active(requestSnapshot), mergedCfg: &config.Config{}, driftAssessment: driftReady})
			model = updated.(Model)
			if strings.Contains(renderErrors(model), "Hook changes:") {
				t.Fatal("unchanged sync retained the previous hook review")
			}
			if strings.Contains(renderErrors(model), "materialize.hook.review") {
				t.Fatal("unchanged sync retained the previous hook review warning")
			}
		})
	}
}

// spec: hook-sync-diagnostics/sync-and-quit-presents-new-hook-review
func TestSyncAndQuitPresentsMaterializationWarnings(t *testing.T) {
	model := NewModel(t.TempDir(), t.TempDir())
	model.loading, model.syncing, model.quitAfterSync = false, true, true
	warning := pipeline.PromoteDiagnostic(diag.Diagnostic{Code: "skill.allowed-tools-omitted", Severity: diag.SeverityWarning, Message: "foreign tool grants omitted"})
	updated, cmd := model.Update(syncDoneMsg{report: &materialize.Report{}, warnings: []error{warning}})
	model = updated.(Model)
	if cmdProducesQuitMsg(cmd) {
		t.Fatal("sync-and-quit discarded a materialization warning")
	}
	updated, _ = model.Update(snapshotLoadedMsg{generation: model.requests.Active(requestSnapshot), mergedCfg: &config.Config{}, driftAssessment: driftReady})
	model = updated.(Model)
	if model.pane != paneErrors || !strings.Contains(renderErrors(model), "skill.allowed-tools-omitted") || model.syncQuitErr != nil {
		t.Fatalf("warning-only sync did not remain reviewable and successful: pane=%v warnings=%v err=%v", model.pane, model.warnings, model.syncQuitErr)
	}
}

// spec: hook-sync-diagnostics/tui-presents-and-retains-completed-hook-review
func TestPreMaterializationFailureKeepsCompletedReviewAndWarnings(t *testing.T) {
	model := NewModel(t.TempDir(), t.TempDir())
	model.loading, model.syncing = false, true
	review := pipeline.PromoteDiagnostic(diag.Diagnostic{Code: diag.CodeMaterializeHookReview, Severity: diag.SeverityWarning, Message: "review changed hooks"})
	report := &materialize.Report{HookChanges: []materialize.HookChange{{Target: "claude", File: "/repo/.claude/settings.json", Event: "Stop", After: json.RawMessage(`{"hooks":[{"type":"command","command":"echo prior-publication"}]}`)}}}
	finish := func(done syncDoneMsg) {
		t.Helper()
		next, _ := model.Update(done)
		model = next.(Model)
		next, _ = model.Update(snapshotLoadedMsg{generation: model.requests.Active(requestSnapshot), mergedCfg: &config.Config{}, driftAssessment: driftReady})
		model = next.(Model)
	}
	finish(syncDoneMsg{report: report, warnings: []error{review}})
	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	model.syncing = true
	warning := errors.New("ancestor exclusion still has materialized output")
	finish(syncDoneMsg{err: errors.New("dependency preflight refused"), warnings: []error{warning}})
	if model.pane != paneList {
		t.Error("preflight failure presented an old review as a new publication")
	}
	next, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = next.(Model)
	for _, want := range []string{"echo prior-publication", "materialize.hook.review", warning.Error(), "dependency preflight refused"} {
		if !strings.Contains(renderErrors(model), want) {
			t.Errorf("preflight failure lost %q:\n%s", want, renderErrors(model))
		}
	}
}

// spec: hook-sync-diagnostics/tui-presents-and-retains-completed-hook-review
func TestToggleFailureKeepsWarningsWithoutReport(t *testing.T) {
	model := NewModel(t.TempDir(), t.TempDir())
	model.loading = false
	id, ok := model.beginOperation(operationMutation)
	if !ok {
		t.Fatal("mutation operation not accepted")
	}
	warning := errors.New("dependency resolution warning")
	next, _ := model.Update(configWrittenMsg{operation: id, err: errors.New("preflight refused"), warnings: []error{warning}})
	model = next.(Model)
	if len(model.warnings) != 1 || !errors.Is(model.warnings[0], warning) {
		t.Fatalf("toggle discarded a warning without a report: %v", model.warnings)
	}
}
