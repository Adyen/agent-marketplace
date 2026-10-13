package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

const (
	recoveryVerificationCopy = "Could not verify the current resource state. Run `agent-marketplace sync --check` in this workspace to see what failed. Fix the reported issue, then press `r` in the resource list to reload."
	recoveryConfigCopy       = "Fix the file and field named above. From the resource list, press `E` to choose a config to edit. Run `agent-marketplace sync --check` in this workspace to check the fix, then press `r` to reload. Ask your administrator to fix organization config errors."
	recoveryCatalogCopy      = "Check access to the source named above. For a local source, check that its directory exists and you can read it; for a remote source, check your network and configured Git or Marketplace credentials. Run `agent-marketplace list` in this workspace to retry discovery, then press `r` in the resource list to reload."
	recoveryResolutionCopy   = "Run `agent-marketplace list` in this workspace to check the available resource names. Fix the selection or source named above, then run `agent-marketplace sync --check`. Press `r` in the resource list to reload."
	recoveryMaterialCopy     = "Run `agent-marketplace doctor` in this workspace to check harness paths. Fix the resource or file error named above, then run `agent-marketplace sync --check`. When the check reports only pending changes, press `s` in the resource list to sync."
	recoveryOperationCopy    = "Run `agent-marketplace sync --check` in this workspace to inspect the current state. Fix the reported issue, then press `r` in the resource list to reload. If the check reports only pending changes, retry sync; retry other failed actions once you fix their cause."
	recoverySnapshotLockCopy = "Check your network and Git access, then run `agent-marketplace sync --locked` in this workspace to fetch the pinned source and sync without changing acquisition locks. Press `r` in the resource list to reload. If verification still fails, keep the lock and ask the registry owner to investigate."
	recoveryManifestLockCopy = "Run `agent-marketplace sync --locked` in this workspace to recheck the pinned source without changing acquisition locks. If verification still fails, ask the registry owner to investigate. Do not delete the lock or run `update` to bypass verification."
	recoveryEditorCopy       = "Check that your configured editor is installed and can open the file named above. For example, start the TUI with `EDITOR=vi agent-marketplace`, then press `E` in the resource list to retry."
	recoveryUnclassifiedCopy = "Run `agent-marketplace sync --check` in this workspace for details. Fix the reported issue, then press `r` in the resource list to reload."
	recoveryInterruptedCopy  = "Run `agent-marketplace sync` in this workspace to finish the interrupted write. Do not use `--check`, `--locked`, or `--offline` for this step. Then press `r` in the resource list to reload."
	recoveryAdminLockCopy    = "Ask your administrator to fix the organization config or acquisition lock named above. Do not edit or delete organization files to bypass the check. After they fix it, run `agent-marketplace sync --check` in this workspace, then press `r` in the resource list to reload."
	recoveryRestoreLockCopy  = "Restore the acquisition lock named above from a known-good copy, or fix its read permissions. Do not delete the lock or run `update` to bypass verification. Run `agent-marketplace sync --check` in this workspace, then press `r` in the resource list to reload."
)

type recoveryRemediable struct {
	hint string
}

func (r recoveryRemediable) Error() string       { return "remediable failure" }
func (r recoveryRemediable) Remediation() string { return r.hint }

func renderedErrorsScreen(m Model) string {
	m.width = 1000
	return ansi.Strip(renderErrors(m))
}

func recoveryErrorsModel(t *testing.T) Model {
	t.Helper()
	return NewModel(t.TempDir(), t.TempDir())
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryCategoryDefaultsRenderBesideCauses(t *testing.T) {
	cases := []struct {
		name     string
		category diagnosticCategory
		want     string
	}{
		{"config", diagnosticConfig, recoveryConfigCopy},
		{"catalog", diagnosticCatalog, recoveryCatalogCopy},
		{"resolution", diagnosticResolution, recoveryResolutionCopy},
		{"materialization", diagnosticMaterialization, recoveryMaterialCopy},
		{"mutation", diagnosticMutation, recoveryOperationCopy},
		{"sync", diagnosticSync, recoveryOperationCopy},
		{"update", diagnosticUpdate, recoveryOperationCopy},
		{"migration", diagnosticMigration, recoveryOperationCopy},
		{"editor", diagnosticEditor, recoveryEditorCopy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := recoveryErrorsModel(t)
			cause := errors.New("plain cause for " + tc.name)
			m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(diagnosticError, tc.category, cause)}, nil, nil)
			m.syncDiagnosticViews()
			screen := renderedErrorsScreen(m)
			if !strings.Contains(screen, "• "+cause.Error()) {
				t.Fatalf("errors screen dropped the original cause:\n%s", screen)
			}
			if !strings.Contains(screen, "→ "+tc.want) {
				t.Fatalf("errors screen missing %s default:\n%s", tc.name, screen)
			}
		})
	}
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryRemediablePrecedenceForErrorsAndWarnings(t *testing.T) {
	cases := []struct {
		name     string
		hint     string
		wantHint string
	}{
		{"nonempty remediation wins", "Use the custom fix.", "Use the custom fix."},
		{"empty remediation falls back", "", recoveryCatalogCopy},
	}
	for _, severity := range []diagnosticSeverity{diagnosticError, diagnosticWarning} {
		for _, tc := range cases {
			t.Run(severityName(severity)+"/"+tc.name, func(t *testing.T) {
				m := recoveryErrorsModel(t)
				cause := recoveryRemediable{hint: tc.hint}
				m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(severity, diagnosticCatalog, cause)}, nil, nil)
				m.syncDiagnosticViews()
				screen := renderedErrorsScreen(m)
				if got := strings.Count(screen, "→ "); got != 1 {
					t.Fatalf("hint lines = %d, want exactly one:\n%s", got, screen)
				}
				if !strings.Contains(screen, "→ "+tc.wantHint) {
					t.Fatalf("errors screen missing %q:\n%s", tc.wantHint, screen)
				}
			})
		}
	}
}

func severityName(severity diagnosticSeverity) string {
	if severity == diagnosticWarning {
		return "warning"
	}
	return "error"
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryOperationFailureSurvivesUntilRetry(t *testing.T) {
	m := recoveryErrorsModel(t)
	cause := errors.New("materialize failed")
	m.recordOperationFailure(operationSync, diagnosticSync, cause)
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "• materialize failed") || !strings.Contains(screen, "→ "+recoveryOperationCopy) {
		t.Fatalf("operation failure lacks cause or recovery copy:\n%s", screen)
	}

	m.replaceSnapshotDiagnostics(nil, nil, nil)
	m.syncDiagnosticViews()
	if len(m.errs) != 1 || !errors.Is(m.errs[0], cause) {
		t.Fatalf("healthy reload erased historical operation failure: %v", m.errs)
	}

	m.recordOperationSuccess(operationSync)
	if len(m.errs) != 0 {
		t.Fatalf("successful retry kept operation failure: %v", m.errs)
	}
}

// spec: tui-error-recovery/acquisition-lock-guidance-respects-scope-and-verification
func TestErrorRecoveryLockSnapshotAndManifestGetSyncGuidance(t *testing.T) {
	cases := []struct {
		name  string
		code  string
		scope lockfile.Scope
		want  string
	}{
		{"snapshot unavailable repo", "lock.snapshot-unavailable", lockfile.ScopeRepo, recoverySnapshotLockCopy},
		{"offline snapshot missing user", "lock.offline-snapshot-missing", lockfile.ScopeUser, recoverySnapshotLockCopy},
		{"offline snapshot missing local", "lock.offline-snapshot-missing", lockfile.ScopeLocal, recoverySnapshotLockCopy},
		{"manifest mismatch local", "lock.manifest-mismatch", lockfile.ScopeLocal, recoveryManifestLockCopy},
		{"manifest mismatch repo", "lock.manifest-mismatch", lockfile.ScopeRepo, recoveryManifestLockCopy},
		{"snapshot unavailable organization stays administrator", "lock.snapshot-unavailable", lockfile.ScopeOrganization, recoveryAdminLockCopy},
		{"manifest mismatch organization stays administrator", "lock.manifest-mismatch", lockfile.ScopeOrganization, recoveryAdminLockCopy},
		{"snapshot unavailable without scope", "lock.snapshot-unavailable", "", recoverySnapshotLockCopy},
		{"offline snapshot missing without scope", "lock.offline-snapshot-missing", "", recoverySnapshotLockCopy},
		{"manifest mismatch without scope", "lock.manifest-mismatch", "", recoveryManifestLockCopy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := recoveryErrorsModel(t)
			cause := fmt.Errorf("resolve: %w", &pipeline.LockViewError{Code: tc.code, Scope: tc.scope, Path: "/workspace/.agents/lock.yaml", Err: errors.New("pinned source")})
			var lockErr *pipeline.LockViewError
			if !errors.As(cause, &lockErr) || lockErr.Code != tc.code {
				t.Fatalf("errors.As lost the lock identity for %s", tc.code)
			}
			m.errs = []error{cause}
			screen := renderedErrorsScreen(m)
			if !strings.Contains(screen, "→ "+tc.want) {
				t.Fatalf("missing sync guidance:\n%s", screen)
			}
			if strings.Contains(screen, "agent-marketplace lock --scope") {
				t.Fatalf("pinned-source failure was offered lock regeneration:\n%s", screen)
			}
		})
	}
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryCompilerHintPromotedFromDiagnosticWins(t *testing.T) {
	m := recoveryErrorsModel(t)
	promoted := pipeline.PromoteDiagnostic(diag.Diagnostic{
		Code:     "skill.test.compiler-hint",
		Resource: "demo",
		Message:  "cannot represent resource",
		Hint:     "Rename the compiler input.",
	})
	m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticMaterialization, promoted)}, nil, nil)
	m.syncDiagnosticViews()
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "→ Rename the compiler input.") {
		t.Fatalf("promoted compiler hint missing:\n%s", screen)
	}
	if strings.Contains(screen, recoveryMaterialCopy) {
		t.Fatalf("materialization fallback displayed beside compiler hint:\n%s", screen)
	}
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryWrappedTypedCausesGetRecoveryCopy(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "wrapped organization trust",
			err:  fmt.Errorf("load: %w", &config.OrganizationTrustError{Code: "trust.owner", Path: "/etc/agent-marketplace/config.yaml", Err: errors.New("owner")}),
			want: recoveryAdminLockCopy,
		},
		{
			name: "wrapped interrupted transaction",
			err:  fmt.Errorf("sync: %w", dependencytxn.ErrRecoveryRequired),
			want: recoveryInterruptedCopy,
		},
		{
			name: "organization missing lock",
			err:  &pipeline.LockViewError{Code: "lock.missing", Scope: lockfile.ScopeOrganization, Path: "/etc/agent-marketplace/lock.yaml", Err: os.ErrNotExist},
			want: recoveryAdminLockCopy,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := recoveryErrorsModel(t)
			m.errs = []error{tc.err}
			screen := renderedErrorsScreen(m)
			if !strings.Contains(screen, "→ "+tc.want) {
				t.Fatalf("errors screen missing typed recovery copy:\n%s", screen)
			}
		})
	}
}

// spec: tui-error-recovery/acquisition-lock-guidance-respects-scope-and-verification
func TestErrorRecoveryOrganizationLockGetsAdministratorHint(t *testing.T) {
	m := recoveryErrorsModel(t)
	m.errs = []error{&pipeline.LockViewError{Code: "lock.stale", Scope: lockfile.ScopeOrganization, Path: "/etc/agent-marketplace/lock.yaml", Err: errors.New("lock does not match configuration")}}
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "→ "+recoveryAdminLockCopy) {
		t.Fatalf("organization lock did not get the administrator hint:\n%s", screen)
	}
	if strings.Contains(screen, "agent-marketplace lock --scope") {
		t.Fatalf("organization lock was offered a regeneration command:\n%s", screen)
	}
}

// spec: tui-error-recovery/acquisition-lock-guidance-respects-scope-and-verification
func TestErrorRecoveryMissingOrStaleWritableLockGetsScopedCommand(t *testing.T) {
	for _, scope := range []lockfile.Scope{lockfile.ScopeRepo, lockfile.ScopeUser, lockfile.ScopeLocal} {
		for _, code := range []string{"lock.missing", "lock.stale"} {
			t.Run(string(scope)+"/"+code, func(t *testing.T) {
				m := recoveryErrorsModel(t)
				m.errs = []error{&pipeline.LockViewError{Code: code, Scope: scope, Path: "/workspace/.agents/lock.yaml", Err: os.ErrNotExist}}
				screen := renderedErrorsScreen(m)
				want := fmt.Sprintf("Run `agent-marketplace lock --scope %s` in this workspace to create or reconcile this acquisition lock without upgrading a current pin. Then run `agent-marketplace sync --check` and press `r` in the resource list to reload.", scope)
				if !strings.Contains(screen, "→ "+want) {
					t.Fatalf("missing scoped lock command:\n%s", screen)
				}
				if strings.Contains(screen, "--upgrade") {
					t.Fatalf("lock guidance offered an upgrade:\n%s", screen)
				}
			})
		}
	}
}

// spec: tui-error-recovery/acquisition-lock-guidance-respects-scope-and-verification
func TestErrorRecoveryInvalidOrUnreadableLockNeverSuggestsBypass(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"invalid", &pipeline.LockViewError{Code: "lock.invalid", Scope: lockfile.ScopeUser, Path: "/home/u/.agents/lock.yaml", Err: errors.New("decode")}},
		{"read", &pipeline.LockViewError{Code: "lock.read", Scope: lockfile.ScopeRepo, Path: "/workspace/.agents/lock.yaml", Err: errors.New("permission denied")}},
		{"lookup", &pipeline.LockViewError{Code: "lock.lookup", Scope: lockfile.ScopeRepo, Err: errors.New("no unambiguous exact lock")}},
		{"standalone lockfile error", &lockfile.Error{Code: lockfile.CodeInvalidDigest, Err: errors.New("digest mismatch")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := recoveryErrorsModel(t)
			m.errs = []error{tc.err}
			screen := renderedErrorsScreen(m)
			if !strings.Contains(screen, "→ "+recoveryRestoreLockCopy) {
				t.Fatalf("invalid or unreadable lock did not get restore guidance:\n%s", screen)
			}
			if strings.Contains(screen, "agent-marketplace lock --scope") {
				t.Fatalf("invalid lock was offered regeneration:\n%s", screen)
			}
		})
	}
}

// spec: tui-error-recovery/interrupted-writes-receive-the-supported-recovery-command
func TestErrorRecoveryDiscoveryEndpointUsesWrappedRecoveryCause(t *testing.T) {
	m := recoveryErrorsModel(t)
	m.discoveryErrs = []error{marketplace.EndpointError{
		Marketplace: "corp",
		URL:         "https://marketplace.example.invalid",
		Err:         dependencytxn.ErrRecoveryRequired,
	}}
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "→ "+recoveryInterruptedCopy) {
		t.Fatalf("discovery endpoint did not show the interrupted-write recovery:\n%s", screen)
	}
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryDiscoveryFailureGetsCatalogGuidance(t *testing.T) {
	m := recoveryErrorsModel(t)
	m.discoveryErrs = []error{marketplace.EndpointError{
		Marketplace: "corp",
		URL:         "https://marketplace.example.invalid",
		Err:         errors.New("connection refused"),
	}}
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "→ "+recoveryCatalogCopy) {
		t.Fatalf("discovery failure did not get catalog guidance:\n%s", screen)
	}
}

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
func TestErrorRecoveryUnclassifiedLegacyErrorsAvoidConfigHints(t *testing.T) {
	m := recoveryErrorsModel(t)
	m.errs = []error{errors.New("legacy failure")}
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "→ "+recoveryUnclassifiedCopy) {
		t.Fatalf("unclassified error did not get the generic guidance:\n%s", screen)
	}
	if strings.Contains(screen, "to choose a config to edit") {
		t.Fatalf("unclassified error got config-specific guidance:\n%s", screen)
	}
}

// spec: tui-error-recovery/recovery-hints-cannot-control-the-terminal
func TestErrorRecoveryHintCannotControlTerminal(t *testing.T) {
	m := recoveryErrorsModel(t)
	m.width = 100
	m.height = 30
	m.errs = []error{recoveryRemediable{hint: "fix\nErrors (99)\x1b]2;owned\x07\u202e"}}
	m.pane = paneErrors
	output := m.View().Content
	for _, unsafe := range []string{"\x1b]2;owned", "\u202e", "\nErrors (99)"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("hint emitted %q: %q", unsafe, output)
		}
	}
	if !strings.Contains(ansi.Strip(output), `fix\x0AErrors (99)\x1B]2;owned\x07\u202E`) {
		t.Fatalf("hint omitted readable escapes: %s", ansi.Strip(output))
	}
}

// spec: tui-error-recovery/failed-verification-provides-a-diagnostic-command
func TestErrorRecoveryUnverifiedSyncReloadShowsDiagnosticCommand(t *testing.T) {
	m := beginSyncOperation(t)

	next, _ := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	reload := m.requests.active(requestSnapshot)
	if reload == 0 {
		t.Fatal("pipeline completion did not schedule an authoritative reload")
	}

	next, _ = m.Update(snapshotLoadedMsg{generation: reload, driftAssessment: driftFailed})
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("failed verification left the sync running: syncing %t progress %v", m.syncing, m.progress)
	}
	if !strings.Contains(m.toast, recoveryVerificationCopy) {
		t.Fatalf("toast missing plain fallback and command: %q", m.toast)
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("unverified reload claimed success: %q", m.toast)
	}
	screen := renderedErrorsScreen(m)
	if !strings.Contains(screen, "• "+recoveryVerificationCopy) {
		t.Fatalf("errors screen missing plain fallback and command:\n%s", screen)
	}
	if got := strings.Count(screen, recoveryVerificationCopy); got != 1 {
		t.Fatalf("verification fallback shown %d times, want once:\n%s", got, screen)
	}
	if strings.Contains(screen, "→ ") {
		t.Fatalf("verification fallback duplicated as a second hint:\n%s", screen)
	}
}

// spec: tui-error-recovery/failed-verification-provides-a-diagnostic-command
func TestErrorRecoverySnapshotAcceptancePreservesJoinedCauses(t *testing.T) {
	recovery := dependencytxn.ErrRecoveryRequired
	missing := os.ErrNotExist

	err := snapshotAcceptanceError(snapshotLoadedMsg{errs: []error{recovery, missing}})
	if !errors.Is(err, recovery) || !errors.Is(err, missing) {
		t.Fatalf("joined causes lost identity: %v", err)
	}

	err = snapshotAcceptanceError(snapshotLoadedMsg{diagnostics: []tuiDiagnostic{
		snapshotDiagnostic(diagnosticError, diagnosticCatalog, recovery),
	}})
	if !errors.Is(err, recovery) {
		t.Fatalf("diagnostic cause lost identity: %v", err)
	}
}
