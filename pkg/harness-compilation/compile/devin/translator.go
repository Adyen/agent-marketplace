package devin

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Canonical Devin 3000.3 output path conventions, relative to a target root
// (`<repo>/.devin` for project scope, `~/.config/devin` for user scope). They
// are deliberately unexported: they are this package's internal path contract,
// not public API, and no sibling target package exports such constants.
const (
	skillsDir      = "skills"
	subagentsDir   = "agents"
	skillFile      = "SKILL.md"
	subagentSuffix = ".md"
	projectHooks   = "hooks.v1.json"
	userConfig     = "config.json"
	mcpConfig      = "mcp_config.json"
)

// Compiler is the Devin target compiler. It covers every marketplace-selectable
// resource kind: skills and converted commands (work package D4), flat custom
// subagents (D5), command/prompt hook fragments (D6), and stdio/HTTP MCP server
// fragments (D7). Rules are out of scope for this target and are dropped with a
// warning; see translateRule for why that is a scope decision rather than a
// capability limit.
type Compiler struct{}

func init() {
	compile.Register(Compiler{})
}

// Target returns Devin as the compiler's target.
func (Compiler) Target() compile.Target { return mapping.TargetDevin }

// Translate compiles one resource with default options.
func (c Compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compile.Options{})
}

// TranslateWithOptions is option-aware so the per-kind Devin compilers can add
// managed-aggregate and path options without changing the registration surface.
//
// Compile start/completion logging for every target and kind is already emitted
// by compile.Compile, so this translator only logs its own mapping decisions.
func (c Compiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	kind, name, err := ir.ValidateResource(r)
	if err != nil {
		return nil, nil, err
	}

	switch resource := r.(type) {
	case *ir.Skill:
		return translateSkill(resource, opts)
	case *ir.Command:
		return translateCommand(resource, opts)
	case *ir.Subagent:
		return translateSubagent(resource, opts)
	case *ir.Hook:
		return translateHook(resource, opts)
	case *ir.McpServer:
		return translateMcpServer(resource, opts)
	case *ir.Rule:
		return translateRule(resource, name)
	}

	// D7 removed the transient `compile.devin.resource-pending` scaffold along
	// with its dispatch branch: every marketplace-selectable kind is now
	// compiled above. An unrecognized kind is a programming error, not a loss
	// to report, so it fails the call the way the sibling targets do rather
	// than being silently absorbed by a catch-all warning diagnostic.
	return nil, nil, fmt.Errorf("devin: unsupported resource kind %v (%s)", kind, name)
}

// translateRule drops the rule with a warning.
//
// This target emits nothing for a rule because rules are out of scope for it,
// NOT because Devin cannot read one. The D12 runtime probe
// (internal/devinprobe.TestProbeRulesDiscoveryUnderDevinRoots) established on
// Devin 3000.3.27 that `devin rules paths` advertises only `.windsurf/rules`
// and `.cursor/rules`, yet a file written to `.devin/rules/<name>.md` IS
// discovered and listed by `devin rules list`. The earlier rationale — "Devin
// has no rules directory primitive" — is therefore false and no longer stated.
//
// Two consequences follow, and both are deliberate:
//
//   - The scope decision stands. Rules are not marketplace-selectable at all
//     (`internal/config.AllKinds` has no rule kind) and "marketplace-selectable
//     rules" is an explicit non-goal of the promoted plan — a non-goal about
//     the selection model, which no runtime observation can falsify. Emitting
//     into `.devin/rules` would additionally mean materializing into a path
//     Devin does not advertise, which is exactly the undocumented behaviour
//     this target refuses to depend on elsewhere (see the documented-transport
//     and closed-tool-vocabulary rules).
//   - The severity is a WARNING, matching `rule.claude.no-primitive` and
//     `rule.gemini.no-primitive`. Every Devin ERROR code in this package marks
//     output that would be actively wrong if emitted — a broadened privilege,
//     an unenforced restriction, a silent transport fallback, a shadowed
//     built-in. Dropping a rule is none of those: nothing unsafe happens, no
//     aggregate entry is at risk, and the loss is identical in kind to the one
//     Claude and Gemini report as a warning. Failing the whole sync over a
//     scope decision would also overstate the situation, since a user can
//     place the same file by hand and Devin will read it.
func translateRule(rule *ir.Rule, name string) ([]artifact.Artifact, []diag.Diagnostic, error) {
	slog.Warn("devin compile rule out of scope", "target", mapping.TargetDevin, "resource", name)
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeRuleDevinUnsupported,
		Severity: diag.SeverityWarning,
		Target:   string(mapping.TargetDevin),
		Resource: name,
		Message:  "Rules are out of scope for the Devin target; no rule artifact was emitted",
		Hint:     "This is a scope decision, not a Devin limitation: on 3000.3.27 a rule placed at .devin/rules/<name>.md is discovered even though `devin rules paths` does not advertise it. Compile the rule to a target with a supported rules primitive (factory, cursor, windsurf), or place it under .devin/rules by hand",
		Source:   compile.DiagSource(rule.Source),
	}}, nil
}
