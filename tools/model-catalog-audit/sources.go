package main

var coverage = map[string]string{
	"claude":   "Current Claude API IDs plus model/effort field documentation. Provider-specific availability and older models are not exhaustively enumerated.",
	"codex":    "ModelDetails IDs from public Codex model documentation plus subagent reasoning documentation. Rollout and authentication affect availability.",
	"cursor":   "Partial: display-name inventory and subagent model/parameter documentation are monitored for changes, not translated into guessed native IDs.",
	"devin":    "Partial: public model and resource-field documentation is monitored for changes. No public exhaustive concrete-ID inventory; unchanged docs do not certify current concrete models or variants.",
	"factory":  "Public native model IDs and reasoning metadata plus custom-droid model/effort field documentation. Session reasoning levels do not prove custom-droid acceptance.",
	"gemini":   "The declared VALID_GEMINI_MODELS set on the upstream main branch plus the subagent model field. Preview/experimental constants are review candidates, not proof of stable release or account availability.",
	"windsurf": "Not applicable: the shipped catalog declares no model-bearing resource surface. No model inventory is claimed.",
}

var sources = append([]source{
	{Name: "claude-models", Harness: "claude", URL: "https://platform.claude.com/docs/en/about-claude/models/overview.md", Kind: "claude"},
	{Name: "claude-subagents", Harness: "claude", URL: "https://code.claude.com/docs/en/sub-agents.md", Kind: "watch", Fields: []string{"model", "effort"}},
	{Name: "claude-skills", Harness: "claude", URL: "https://code.claude.com/docs/en/skills.md", Kind: "watch", Fields: []string{"model"}},
	{Name: "codex-models", Harness: "codex", URL: "https://developers.openai.com/codex/models.md", Kind: "codex"},
	{Name: "codex-subagents", Harness: "codex", URL: "https://developers.openai.com/codex/subagents.md", Kind: "watch", Sections: []string{"Choosing models and reasoning"}},
	{Name: "cursor-models", Harness: "cursor", URL: "https://cursor.com/docs/models-and-pricing.md", Kind: "cursor"},
	{Name: "cursor-subagents", Harness: "cursor", URL: "https://cursor.com/docs/subagents.md", Kind: "watch", Fields: []string{"model"}, Sections: []string{"Model configuration"}},
	{Name: "devin-models", Harness: "devin", URL: "https://docs.devin.ai/cli/models.md", Kind: "watch", Sections: []string{"Available Models"}},
	{Name: "devin-subagents", Harness: "devin", URL: "https://docs.devin.ai/cli/subagents.md", Kind: "watch", Fields: []string{"model"}},
	{Name: "devin-skills", Harness: "devin", URL: "https://docs.devin.ai/cli/extensibility/skills/creating-skills.md", Kind: "watch", Fields: []string{"model"}},
	{Name: "factory-models", Harness: "factory", URL: "https://docs.factory.ai/docs/models.md", Kind: "factory"},
	{Name: "factory-subagents", Harness: "factory", URL: "https://docs.factory.ai/harness/subagents.md", Kind: "watch", Fields: []string{"model", "reasoningEffort"}},
	{Name: "gemini-models", Harness: "gemini", URL: "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/core/src/config/models.ts", Kind: "gemini"},
	{Name: "gemini-subagents", Harness: "gemini", URL: "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/core/subagents.md", Kind: "watch", Fields: []string{"model"}},
}, hookToolSources...)
