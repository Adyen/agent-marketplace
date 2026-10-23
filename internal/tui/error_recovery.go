package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/remedy"
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

// structuredDiagnostic is implemented by errors whose Error text embeds their
// own remediation lines, such as the update selection refusal. The errors
// screen shows the cause as the diagnostic and the remediation as its recovery
// line, each on one line, instead of flattening the embedded line breaks.
type structuredDiagnostic interface {
	error
	DiagnosticCause() string
	DiagnosticRemediation() string
}

// diagnosticCause is the diagnostic text the TUI shows for err: its Error
// text, with a structured diagnostic's embedded remediation removed while any
// wrapping prefix, such as "update refresh failed: ", is kept.
func diagnosticCause(err error) (text string) {
	// fmt.Sprint renders a typed nil error as "<nil>" instead of panicking;
	// unwrapping one can still panic, so fall back to that text.
	text = fmt.Sprint(err)
	defer func() {
		if recover() != nil {
			text = fmt.Sprint(err)
		}
	}()
	var structured structuredDiagnostic
	if errors.As(err, &structured) {
		if cause := strings.TrimSpace(structured.DiagnosticCause()); cause != "" {
			return strings.Replace(text, structured.Error(), cause, 1)
		}
	}
	return text
}

func diagnosticRemediation(err error, category diagnosticCategory) string {
	var structured structuredDiagnostic
	if errors.As(err, &structured) {
		if hint := strings.TrimSpace(structured.DiagnosticRemediation()); hint != "" {
			return hint
		}
	}
	var remediable remedy.Remediable
	if errors.As(err, &remediable) {
		if hint := remediable.Remediation(); hint != "" {
			return hint
		}
	}
	if errors.Is(err, errResourceStateUnverified) {
		return ""
	}
	if hint := remedy.For(err, ""); hint != "" {
		return appendCatalogReload(hint)
	}
	if hint := typedRecoveryRemediation(err); hint != "" {
		return hint
	}
	return categoryRecoveryRemediation(category)
}

func appendCatalogReload(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	if !strings.HasSuffix(hint, ".") {
		hint += "."
	}
	return hint + " Then press `r` in the resource list to reload."
}

func typedRecoveryRemediation(err error) string {
	if errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		return interruptedWriteRecoveryCopy
	}
	var trustErr *config.OrganizationTrustError
	if errors.As(err, &trustErr) {
		return organizationLockRecoveryCopy
	}
	var lockfileErr *lockfile.Error
	if errors.As(err, &lockfileErr) {
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
