package materialize

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// sharedPathDiagnostics reports each resource that lost a native path it
// shares with another resource: one of another kind with the same installed
// name, such as a command and a skill on a target that installs commands as
// skills, or one whose name differs only in letter case.
func sharedPathDiagnostics(displaced []resolver.SharedPathDisplacement) []diag.Diagnostic {
	if len(displaced) == 0 {
		return nil
	}
	diags := make([]diag.Diagnostic, 0, len(displaced))
	for _, d := range displaced {
		winner := fmt.Sprintf("%s %q", skill.KindLabel(d.Winner.ResourceKind()), d.Winner.MaterializedName())
		loser := fmt.Sprintf("%s %q", skill.KindLabel(d.Displaced.ResourceKind()), d.Displaced.MaterializedName())
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
		precedence := winner
		if d.Winner.Required {
			precedence = "required " + winner
		}
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeMaterializeSharedPathCollision,
			Severity: diag.SeverityWarning,
			Target:   string(d.Target),
			Resource: d.Displaced.MaterializedName(),
			Message: fmt.Sprintf("%s from %s and %s from %s %s for %s in %s scope; %s takes precedence and %s was not installed there",
				winner, redact.URL(winnerSource), loser, redact.URL(displacedSource), where, d.Target, scope, precedence, loser),
			Hint: "Rename one of the resources or exclude the one you do not want on this target; targets with separate paths keep both",
		})
	}
	return diags
}
