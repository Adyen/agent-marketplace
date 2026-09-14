package devin

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/rulefmt"
)

// Canonical Devin 3000.3 output path conventions, relative to a general target
// root (`<repo>/.devin` for project scope, `~/.config/devin` for user scope).
// Rules are the exception: materialization routes project rules under the
// project root and machine rules under the resource-specific `~/.devin` root.
// These constants
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
// resource kind: skills and converted commands, flat custom subagents,
// command/prompt hook fragments, stdio/HTTP MCP server fragments, and native
// trigger-based Markdown rules.
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
	return compile.GuardNativeCodexTranslation(r, c.Target(), func(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
		return c.translateWithOptions(r, opts)
	})
}

func (Compiler) translateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
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

// translateRule emits Devin's trigger-based rules/<name>.md artifact.
// Materialization routes project output to .devin/rules and machine output to
// ~/.devin/rules.
func translateRule(rule *ir.Rule, name string) ([]artifact.Artifact, []diag.Diagnostic, error) {
	body, trigger, diags, err := rulefmt.RenderWithDiagnostics(rule, mapping.TargetDevin)
	if err != nil {
		return nil, nil, fmt.Errorf("devin: %w", err)
	}
	slog.Debug("devin rule compiled", "target", mapping.TargetDevin, "resource", name, "trigger", trigger)
	return []artifact.Artifact{{Path: "rules/" + name + ".md", Bytes: body}}, diags, nil
}
