package resourcediff

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestDefinitionSkillIncludesSemanticMetadataBodyAndExtensions(t *testing.T) {
	skill := ir.NewSkillWithOptions("deploy-service", "Deploy the service", "Run the deploy steps.\nVerify the release.", []string{"user"}, map[string]any{"x-org": "adyen"}, ir.SourceRef{File: "skills/deploy-service/SKILL.md"},
		ir.WithSkillAllowedTools([]string{"bash", "git"}),
		ir.WithSkillModel("sonnet"),
	)
	path, doc, err := Definition(skill)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if path != "SKILL.md" {
		t.Fatalf("review path = %q, want SKILL.md", path)
	}
	// The expectation below is authored independently: it checks the exact
	// normalized review document, not a re-derivation.
	want := `{
  "allowed-tools": [
    "bash",
    "git"
  ],
  "description": "Deploy the service",
  "extra": {
    "x-org": "adyen"
  },
  "model": "sonnet",
  "name": "deploy-service",
  "triggers": [
    "user"
  ]
}

Run the deploy steps.
Verify the release.
`
	if doc != want {
		t.Fatalf("normalized skill definition mismatch:\ngot:\n%s\nwant:\n%s", doc, want)
	}
	if !strings.Contains(doc, "Run the deploy steps.\n") {
		t.Fatalf("body must remain multiline markdown text:\n%s", doc)
	}
}

func TestDefinitionPreservesExplicitEmptyAndUnsetDistinctions(t *testing.T) {
	withEmpty := ir.NewSkillWithOptions("empty-tools", "d", "body", nil, nil, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{}))
	_, doc, err := Definition(withEmpty)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(doc, `"allowed-tools": []`) {
		t.Fatalf("explicitly empty allowed-tools must be visible as []:\n%s", doc)
	}
	absent := ir.NewSkill("no-tools", "d", "body", nil, nil, ir.SourceRef{})
	_, doc, err = Definition(absent)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if strings.Contains(doc, "allowed-tools") {
		t.Fatalf("absent allowed-tools must not appear:\n%s", doc)
	}
}

func TestDefinitionHookShowsExecutableDeclarationAndPreservedExtras(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "python3 hooks/check.py", "command", map[string]any{"x-note": "keep"}, ir.SourceRef{}, ir.WithHookPrompt("Check first"), ir.WithHookTimeoutPtr(intPtr(30)))
	hook.Ext.Windsurf = &ext.Windsurf{HookShowOutput: boolPtr(true)}
	path, doc, err := Definition(hook)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if path != "HOOK.md" {
		t.Fatalf("review path = %q, want HOOK.md", path)
	}
	for _, want := range []string{
		`"command": "python3 hooks/check.py"`,
		`"event": "PreToolUse"`,
		`"matcher": "Bash"`,
		`"prompt": "Check first"`,
		`"timeout": 30`,
		`"x-note": "keep"`,
		`"windsurf"`,
		`"hookShowOutput": true`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("hook definition missing %s:\n%s", want, doc)
		}
	}
}

func TestDefinitionMcpServerShowsConfigurationValues(t *testing.T) {
	server := ir.NewMcpServerWithOptions("fetcher", "npx", []string{"-y", "fetch-server"}, map[string]string{"TOKEN": "literal-authored"}, map[string]any{}, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpDisabled(false))
	path, doc, err := Definition(server)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if path != ".mcp.json" {
		t.Fatalf("review path = %q, want .mcp.json", path)
	}
	for _, want := range []string{
		`"command": "npx"`,
		`"args": [`,
		`"-y"`,
		`"env": {`,
		`"TOKEN": "literal-authored"`,
		`"url": "https://example.test/mcp"`,
		`"disabled": false`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("mcp definition missing %s:\n%s", want, doc)
		}
	}
}

func TestDefinitionSubagentAndCollection(t *testing.T) {
	subagent := ir.NewSubagentWithOptions("reviewer", "Reviews code", "Review the change carefully.", "sonnet", []string{"read", "grep"}, "high", nil, "", map[string]any{}, ir.SourceRef{}, ir.WithSubagentMaxNesting(2))
	path, doc, err := Definition(subagent)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if path != "AGENT.md" {
		t.Fatalf("review path = %q, want AGENT.md", path)
	}
	if !strings.Contains(doc, `"max-nesting": 2`) || !strings.Contains(doc, "Review the change carefully.") {
		t.Fatalf("subagent definition incomplete:\n%s", doc)
	}

	collection := ir.NewCollection("bundle", "2.0", "A bundle", []ir.CollectionMember{{Name: "deploy", Kind: ir.KindSkill}, {Name: "guard", Kind: ir.KindHook}}, []string{"other"}, map[string]any{}, ir.SourceRef{})
	path, doc, err = Definition(collection)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if path != "COLLECTION.json" {
		t.Fatalf("review path = %q, want COLLECTION.json", path)
	}
	for _, want := range []string{
		`"version": "2.0"`,
		`"members": [`,
		`"kind": "skill"`,
		`"kind": "hook"`,
		`"includes": [`,
		`"other"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("collection definition missing %s:\n%s", want, doc)
		}
	}
}

// spec: update-output/equivalent-git-and-marketplace-definitions-use-the-same-review-view
func TestEquivalentResourcesRenderIdenticalReviewViews(t *testing.T) {
	// Two resources that carry the same semantics but were produced through
	// different ingestion paths: one built with a different description
	// formatting/source location and Extra key ordering.
	gitSide := ir.NewSkillWithOptions("deploy", "Deploy it", "Body text.", []string{"user"}, map[string]any{"b-extra": "two", "a-extra": "one"}, ir.SourceRef{File: "skills/deploy/SKILL.md", Line: 3})
	marketplaceSide := ir.NewSkillWithOptions("deploy", "Deploy it", "Body text.", []string{"user"}, map[string]any{"a-extra": "one", "b-extra": "two"}, ir.SourceRef{File: "payload/skill.json"})
	_, gitDoc, err := Definition(gitSide)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	_, mpDoc, err := Definition(marketplaceSide)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if gitDoc != mpDoc {
		t.Fatalf("equivalent resources must render identical review views:\ngit:\n%s\nmarketplace:\n%s", gitDoc, mpDoc)
	}
	// Source locations must not leak into the review view.
	if strings.Contains(gitDoc, "SKILL.md") || strings.Contains(gitDoc, "payload") {
		t.Fatalf("source location leaked into normalized definition:\n%s", gitDoc)
	}
}

// spec: update-output/authored-values-are-shown-without-resolving-local-secrets
func TestDefinitionShowsAuthoredLiteralReferencesWithoutEnvExpansion(t *testing.T) {
	t.Setenv("AM_DIFF_TEST_SECRET", "local-machine-secret-value")
	server := ir.NewMcpServerWithOptions("runner", "sh", []string{"-c", "printenv AUTHORED_TOKEN"}, map[string]string{"AUTHORED_TOKEN": "${AM_DIFF_TEST_SECRET}"}, nil, ir.SourceRef{})
	_, doc, err := Definition(server)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(doc, `${AM_DIFF_TEST_SECRET}`) {
		t.Fatalf("authored environment reference must be shown literally:\n%s", doc)
	}
	if strings.Contains(doc, "local-machine-secret-value") {
		t.Fatalf("local environment value must never be expanded into review content:\n%s", doc)
	}
}

// spec: update-output/terminal-controls-and-locator-credentials-are-not-emitted-unsafely
func TestSanitizeReviewLineNeutralizesTerminalControls(t *testing.T) {
	got := SanitizeReviewLine("bad\rbidi\u202eflow\vctrl\fliteral")
	if !strings.Contains(got, "literal") {
		t.Fatalf("readable text must survive sanitizing: %q", got)
	}
	for _, danger := range []string{"\r", "\v", "\f", "\u202e"} {
		if strings.ContainsRune(got, []rune(danger)[0]) {
			t.Fatalf("terminal control %q must be neutralized: %q", danger, got)
		}
	}
	// ANSI escapes never survive: they are rejected earlier by the text
	// gate, and sanitizing must be deterministic.
	if SanitizeReviewLine("plain") != "plain" {
		t.Fatalf("plain lines must pass through unchanged: %q", SanitizeReviewLine("plain"))
	}
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
