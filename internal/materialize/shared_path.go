package materialize

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// sharedPathDiagnostics reports each resource that lost a native path to a
// resource of another kind with the same installed name, for example a command
// and a skill on a target that installs commands as skills.
func sharedPathDiagnostics(displaced []resolver.SharedPathDisplacement) []diag.Diagnostic {
	if len(displaced) == 0 {
		return nil
	}
	diags := make([]diag.Diagnostic, 0, len(displaced))
	for _, d := range displaced {
		winnerKind, displacedKind := skill.KindLabel(d.Winner.ResourceKind()), skill.KindLabel(d.Displaced.ResourceKind())
		winnerSource, displacedSource := d.Sources()
		winnerPath, displacedPath := d.Paths()
		where := "both install to " + winnerPath
		if winnerPath != displacedPath {
			where = fmt.Sprintf("install to %s and %s, one path on case-insensitive filesystems,", winnerPath, displacedPath)
		}
		scope := "repository"
		if d.MachineScope {
			scope = "machine"
		}
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeMaterializeSharedPathCollision,
			Severity: diag.SeverityWarning,
			Target:   string(d.Target),
			Resource: d.Displaced.Name,
			Message: fmt.Sprintf("%s %q from %s and %s %q from %s %s for %s in %s scope; the %s takes precedence and the %s was not installed there",
				winnerKind, d.Winner.Name, redact.URL(winnerSource), displacedKind, d.Displaced.Name, redact.URL(displacedSource), where, d.Target, scope, winnerKind, displacedKind),
			Hint: "Rename one of the resources or exclude the one you do not want on this target; targets with separate paths keep both",
		})
	}
	return diags
}
