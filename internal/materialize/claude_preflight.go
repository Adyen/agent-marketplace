package materialize

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"golang.org/x/text/unicode/norm"
)

// Claude's independently pruned scopes cannot safely share a relocated root.
// Keep other targets eligible even when Claude destination admission fails.
func preflightClaudeDestinations(result *compilePhaseResult, repo string) *diag.Diagnostic {
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		return nil
	}
	machine := outputKey{target: hcc.TargetClaude, machineScope: true}
	project := outputKey{target: hcc.TargetClaude}
	if !result.allOutputs[machine] {
		return nil
	}
	refuse := func(code, message string, both bool) *diag.Diagnostic {
		delete(result.allOutputs, machine)
		if both {
			delete(result.allOutputs, project)
		}
		slog.Error("Claude destination preflight refused", "code", code, "error", message)
		return &diag.Diagnostic{Code: code, Severity: diag.SeverityError, Target: "claude", Message: message,
			Hint: "fix CLAUDE_CONFIG_DIR or move the repository so the Claude destinations are separate"}
	}
	destinations, err := harness.ResolveDestinations(hcc.TargetClaude, true, repo)
	if err != nil {
		return refuse("materialize.destination.error", err.Error(), false)
	}
	if !result.allOutputs[project] {
		return nil
	}
	repoRoot, err := filepath.Abs(filepath.Join(repo, ".claude"))
	if err != nil {
		return refuse("materialize.destination.error", err.Error(), true)
	}
	root := destinations[0].Root
	canonicalRepo, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		return refuse("materialize.destination.error", err.Error(), true)
	}
	canonicalMachine, err := ownership.CanonicalRoot(root)
	if err != nil {
		return refuse("materialize.destination.error", err.Error(), true)
	}
	physical, err := destinationPhysicalRoots([]string{canonicalRepo, canonicalMachine})
	if err != nil {
		return refuse("materialize.destination.error", fmt.Sprintf("inspect Claude destination locations: %v", err), true)
	}
	a := strings.ToLower(norm.NFC.String(physical[canonicalRepo]))
	b := strings.ToLower(norm.NFC.String(physical[canonicalMachine]))
	if a == b || pathIsAncestor(a, b) || pathIsAncestor(b, a) {
		return refuse(diag.CodeMaterializeDestinationOverlap,
			fmt.Sprintf("Claude repository destination %q and machine destination %q overlap", repoRoot, root), true)
	}
	return nil
}

// A relocated root can contain the inactive default account (for example HOME).
// Fence it out of the recursive marker walk, including through a root alias.
func protectInactiveClaudeDefaults(root string, protected map[string]bool) (map[string]bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	physicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		return nil, err
	}
	physicalHome, err := ownership.CanonicalRoot(home)
	if err != nil {
		return nil, err
	}
	if !sameOutputDir(physicalRoot, filepath.Join(physicalHome, ".claude")) {
		protected, _ = addNativeHookPrunePath(protected, physicalRoot, physicalHome, ".claude", true)
	}
	protected, _ = addNativeHookPrunePath(protected, physicalRoot, physicalHome, ".claude.json", false)
	return protected, nil
}
