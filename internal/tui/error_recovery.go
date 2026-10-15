package tui

import (
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

const (
	resourceStateUnverifiedCopy  = "Could not verify the current resource state. Run `agent-marketplace sync --check` in this workspace to see what failed. Fix the reported issue, then press `r` in the resource list to reload."
	configRecoveryCopy           = "Fix the file and field named above. From the resource list, press `E` to choose a config to edit. Run `agent-marketplace sync --check` in this workspace to check the fix, then press `r` to reload. Ask your administrator to fix organization config errors."
	catalogRecoveryCopy          = "Check access to the source named above. For a local source, check that its directory exists and you can read it; for a remote source, check your network and configured Git or Marketplace credentials. Run `agent-marketplace list` in this workspace to retry discovery, then press `r` in the resource list to reload."
	resolutionRecoveryCopy       = "Run `agent-marketplace list` in this workspace to check the available resource names. Fix the selection or source named above, then run `agent-marketplace sync --check`. Press `r` in the resource list to reload."
	materializationRecoveryCopy  = "Run `agent-marketplace doctor` in this workspace to check harness paths. Fix the resource or file error named above, then run `agent-marketplace sync --check`. When the check reports only pending changes, press `s` in the resource list to sync."
	operationRecoveryCopy        = "Run `agent-marketplace sync --check` in this workspace to inspect the current state. Fix the reported issue, then press `r` in the resource list to reload. If the check reports only pending changes, retry sync; retry other failed actions once you fix their cause."
	editorRecoveryCopy           = "Check that your configured editor is installed and can open the file named above. For example, start the TUI with `EDITOR=vi agent-marketplace`, then press `E` in the resource list to retry."
	unclassifiedRecoveryCopy     = "Run `agent-marketplace sync --check` in this workspace for details. Fix the reported issue, then press `r` in the resource list to reload."
	interruptedWriteRecoveryCopy = "Run `agent-marketplace sync` in this workspace to finish the interrupted write. Do not use `--check`, `--locked`, or `--offline` for this step. Then press `r` in the resource list to reload."
	organizationLockRecoveryCopy = "Ask your administrator to fix the organization config or acquisition lock named above. Do not edit or delete organization files to bypass the check. After they fix it, run `agent-marketplace sync --check` in this workspace, then press `r` in the resource list to reload."
	restoreLockRecoveryCopy      = "Restore the acquisition lock named above from a known-good copy, or fix its read permissions. Do not delete the lock or run `update` to bypass verification. Run `agent-marketplace sync --check` in this workspace, then press `r` in the resource list to reload."
	snapshotLockRecoveryCopy     = "Check your network and Git access, then run `agent-marketplace sync --locked` in this workspace to fetch the pinned source and sync without changing acquisition locks. Press `r` in the resource list to reload. If verification still fails, keep the lock and ask the registry owner to investigate."
	manifestLockRecoveryCopy     = "Run `agent-marketplace sync --locked` in this workspace to recheck the pinned source without changing acquisition locks. If verification still fails, ask the registry owner to investigate. Do not delete the lock or run `update` to bypass verification."
)

type resourceStateUnverifiedError struct{}

func (resourceStateUnverifiedError) Error() string { return resourceStateUnverifiedCopy }

var errResourceStateUnverified error = resourceStateUnverifiedError{}

type diagnosticCategoryKey struct {
	severity  diagnosticSeverity
	stableKey string
}

func (m Model) diagnosticCategories() map[diagnosticCategoryKey][]diagnosticCategory {
	categories := make(map[diagnosticCategoryKey][]diagnosticCategory, len(m.diagnostics.snapshot)+len(m.diagnostics.operations))
	for _, diagnostics := range [][]tuiDiagnostic{m.diagnostics.snapshot, m.diagnostics.operations} {
		for _, d := range diagnostics {
			if d.err == nil {
				continue
			}
			key := diagnosticCategoryKey{severity: d.severity, stableKey: stableDiagnosticKey(d.err)}
			categories[key] = append(categories[key], d.category)
		}
	}
	return categories
}

func diagnosticRemediation(err error, category diagnosticCategory) string {
	var remediable resolver.Remediable
	if errors.As(err, &remediable) {
		if hint := remediable.Remediation(); hint != "" {
			return hint
		}
	}
	if errors.Is(err, errResourceStateUnverified) {
		return ""
	}
	if hint := typedRecoveryRemediation(err); hint != "" {
		return hint
	}
	return categoryRecoveryRemediation(category)
}

func typedRecoveryRemediation(err error) string {
	if errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		return interruptedWriteRecoveryCopy
	}
	var trustErr *config.OrganizationTrustError
	if errors.As(err, &trustErr) {
		return organizationLockRecoveryCopy
	}
	var lockErr *pipeline.LockViewError
	if errors.As(err, &lockErr) {
		return lockViewRecoveryRemediation(lockErr)
	}
	var lockfileErr *lockfile.Error
	if errors.As(err, &lockfileErr) {
		return restoreLockRecoveryCopy
	}
	return ""
}

func writableLockScope(scope lockfile.Scope) bool {
	switch scope {
	case lockfile.ScopeRepo, lockfile.ScopeUser, lockfile.ScopeLocal:
		return true
	}
	return false
}

func syncableLockScope(scope lockfile.Scope) bool {
	return scope == "" || writableLockScope(scope)
}

func lockViewRecoveryRemediation(lockErr *pipeline.LockViewError) string {
	if lockErr.Scope == lockfile.ScopeOrganization {
		return organizationLockRecoveryCopy
	}
	switch lockErr.Code {
	case "lock.missing", "lock.stale":
		if writableLockScope(lockErr.Scope) {
			return fmt.Sprintf("Run `agent-marketplace lock --scope %s` in this workspace to create or reconcile this acquisition lock without upgrading a current pin. Then run `agent-marketplace sync --check` and press `r` in the resource list to reload.", lockErr.Scope)
		}
	case "lock.snapshot-unavailable", "lock.offline-snapshot-missing":
		if syncableLockScope(lockErr.Scope) {
			return snapshotLockRecoveryCopy
		}
	case "lock.manifest-mismatch":
		if syncableLockScope(lockErr.Scope) {
			return manifestLockRecoveryCopy
		}
	case "lock.invalid", "lock.read", "lock.lookup":
		return restoreLockRecoveryCopy
	}
	return ""
}

func categoryRecoveryRemediation(category diagnosticCategory) string {
	switch category {
	case diagnosticConfig:
		return configRecoveryCopy
	case diagnosticCatalog:
		return catalogRecoveryCopy
	case diagnosticResolution:
		return resolutionRecoveryCopy
	case diagnosticMaterialization:
		return materializationRecoveryCopy
	case diagnosticMutation, diagnosticSync, diagnosticUpdate:
		return operationRecoveryCopy
	case diagnosticEditor:
		return editorRecoveryCopy
	default:
		return unclassifiedRecoveryCopy
	}
}
