package skill

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type Skill struct {
	Kind        ir.ResourceKind
	Name        string
	Description string
	Tags        []string
	Path        string
}

type MalformedSkill struct {
	DirName string
	Path    string
	Error   error
	// DeclaredName is the frontmatter `name:` value recovered on a best-effort
	// basis from the resource that failed to load. It is the only way to
	// correlate a selection with a dropped resource when the two differ —
	// bundle.parseSkillFromFS prefers frontmatter `name:` over the directory
	// name, so a config selecting the declared name matches neither DirName
	// nor Path. Empty when the name could not be recovered.
	DeclaredName string
	// Kind is the resource kind the failed load was for, as
	// ir.ResourceKind.String() ("skill", "command", "mcpserver", ...). It is
	// empty when the producer could not classify the failure, in which case
	// correlation falls back to matching on name alone.
	//
	// It exists to stop cross-kind misattribution: a malformed skill directory
	// named "foo" says nothing about an MCP server or command named "foo", and
	// blaming it would both point the user at an unrelated file and mask a
	// genuinely stale selection.
	Kind string
}

type catalog map[string]Skill

// MalformedKind returns a malformed resource's classified kind. Missing and
// unknown classifications retain the historical Skill fallback.
func MalformedKind(malformed MalformedSkill) ir.ResourceKind {
	if kind, ok := ParseKind(malformed.Kind); ok && strings.TrimSpace(malformed.Kind) != "" {
		return kind
	}
	return ir.KindSkill
}

func MalformedKindLabel(malformed MalformedSkill) string {
	return KindLabel(MalformedKind(malformed))
}

func KindOf(s Skill) ir.ResourceKind {
	return s.Kind
}

func KindLabel(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "skill"
	case ir.KindCommand:
		return "command"
	case ir.KindSubagent:
		return "subagent"
	case ir.KindHook:
		return "hook"
	case ir.KindMcpServer:
		return "mcp"
	case ir.KindCollection:
		return "collection"
	case ir.KindRule:
		return "rule"
	default:
		return kind.String()
	}
}

func ParseKind(value string) (ir.ResourceKind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return 0, true
	case "skill", "skills":
		return ir.KindSkill, true
	case "command", "commands":
		return ir.KindCommand, true
	case "subagent", "subagents", "agent", "agents":
		return ir.KindSubagent, true
	case "hook", "hooks":
		return ir.KindHook, true
	case "mcp", "mcpserver", "mcpservers", "mcp_server", "mcp_servers":
		return ir.KindMcpServer, true
	case "collection", "collections":
		return ir.KindCollection, true
	case "rule", "rules":
		return ir.KindRule, true
	default:
		return 0, false
	}
}

func ConfigKeyForKind(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "skills"
	case ir.KindCommand:
		return "commands"
	case ir.KindSubagent:
		return "subagents"
	case ir.KindHook:
		return "hooks"
	case ir.KindMcpServer:
		return "mcp"
	case ir.KindCollection:
		return "collections"
	case ir.KindRule:
		return "rules"
	default:
		return ""
	}
}

func ResourceKey(kind ir.ResourceKind, name string) string {
	return KindLabel(kind) + "\x00" + strings.TrimSpace(name)
}
