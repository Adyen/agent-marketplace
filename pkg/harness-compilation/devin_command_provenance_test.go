package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

const devinSiblingSkill = "---\nname: sibling\ndescription: Unmarked sibling skill\n---\nInert sibling body.\n"

func devinFrontmatter(t *testing.T, data []byte) map[string]any {
	t.Helper()
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("frontmatter missing: %q", text)
	}
	block, _, found := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !found {
		t.Fatalf("frontmatter closing delimiter missing: %q", text)
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
		t.Fatalf("native YAML parser rejected frontmatter: %v", err)
	}
	return fields
}

func artifactBytes(result *Result, path string) ([]byte, bool) {
	for _, a := range result.Artifacts {
		if a.Path == path {
			return a.Bytes, true
		}
	}
	return nil, false
}

// spec: harness-compilation/marked-devin-skill-with-controls-the-command-cannot-hold-is-refused
func TestDevinMarkedSkillWithUnholdableControlsIsRefused(t *testing.T) {
	controls := map[string]string{
		"deny policy":         "triggers: [user]\npermissions:\n  deny: [exec]\n",
		"allow-only policy":   "permissions:\n  allow: [read]\n",
		"model-only triggers": "triggers: [model]\n",
		"empty triggers":      "triggers: []\n",
		"agent selector":      "triggers: [user]\nagent: reviewer\n",
		"subagent selector":   "subagent: subagent_explore\n",
	}
	targets := map[Target]struct{ marked, sibling string }{
		TargetDevin:  {"skills/restricted/SKILL.md", "skills/sibling/SKILL.md"},
		TargetClaude: {"commands/restricted.md", "skills/sibling/SKILL.md"},
	}
	for name, control := range controls {
		for target, paths := range targets {
			t.Run(name+"/"+string(target), func(t *testing.T) {
				src := fstest.MapFS{
					".devin/skills/restricted/SKILL.md": {Data: []byte("---\nname: restricted\ndescription: Marked command\nx-agent-marketplace-kind: command\n" + control + "---\nInert command body.\n")},
					".devin/skills/sibling/SKILL.md":    {Data: []byte(devinSiblingSkill)},
				}
				result, err := Convert(src, HarnessDevin, target)
				if err != nil {
					t.Fatal(err)
				}
				refused := false
				for _, d := range result.Diagnostics {
					if d.Code == diag.CodeIngestDevinCommandSecurityUnsupported {
						refused = d.Severity == diag.SeverityError && d.Resource == "restricted"
						if strings.Contains(d.Message, "exec") || strings.Contains(d.Message, "reviewer") || strings.Contains(d.Message, "subagent_explore") {
							t.Fatalf("diagnostic quotes a control value: %q", d.Message)
						}
					}
				}
				if !refused {
					t.Fatalf("marked skill was not refused: %+v", result.Diagnostics)
				}
				if data, ok := artifactBytes(result, paths.marked); ok {
					t.Fatalf("refused resource still published %s:\n%s", paths.marked, data)
				}
				for _, a := range result.Artifacts {
					if strings.Contains(string(a.Bytes), "Inert command body.") {
						t.Fatalf("refused resource body reached %s", a.Path)
					}
				}
				if _, ok := artifactBytes(result, paths.sibling); !ok {
					t.Fatalf("unmarked sibling skill lost: %+v", result.Artifacts)
				}
			})
		}
	}
}

// spec: harness-compilation/activation-that-includes-user-narrows-to-a-converted-command
func TestDevinUserActivationNarrowsToConvertedCommand(t *testing.T) {
	const doc = "---\nname: review\ndescription: Review the diff\nx-agent-marketplace-kind: command\ntriggers:\n  - user\n  - model\n---\nInert command body.\n"
	src := fstest.MapFS{".devin/skills/review/SKILL.md": {Data: []byte(doc)}}
	result, err := Convert(src, HarnessDevin, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("narrowable activation refused: %+v", result.Diagnostics)
	}
	narrowed := false
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeIngestDevinCommandFieldDropped && d.Severity == diag.SeverityWarning && d.Resource == "review" {
			narrowed = true
		}
	}
	if !narrowed {
		t.Fatalf("narrowed activation not reported: %+v", result.Diagnostics)
	}
	data, ok := artifactBytes(result, "skills/review/SKILL.md")
	if !ok {
		t.Fatalf("converted command skill missing: %+v", result.Artifacts)
	}
	fields := devinFrontmatter(t, data)
	triggers, _ := fields["triggers"].([]any)
	if len(triggers) != 1 || triggers[0] != "user" || fields["x-agent-marketplace-kind"] != "command" {
		t.Fatalf("activation not narrowed to a user-only converted command: %#v", fields)
	}
}

// spec: harness-compilation/plain-converted-command-skill-still-round-trips
func TestDevinPlainConvertedCommandSkillRoundTrips(t *testing.T) {
	const doc = "---\nname: review\ndescription: Review the diff\nx-agent-marketplace-kind: command\ntriggers:\n  - user\nallowed-tools:\n  - read\n---\nInert command body.\n"
	src := fstest.MapFS{".devin/skills/review/SKILL.md": {Data: []byte(doc)}}
	result, err := Convert(src, HarnessDevin, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("plain converted command skill refused: %+v", result.Diagnostics)
	}
	data, ok := artifactBytes(result, "skills/review/SKILL.md")
	if !ok {
		t.Fatalf("converted command skill missing: %+v", result.Artifacts)
	}
	if string(data) != doc {
		t.Fatalf("round trip changed the converted command skill:\n%s\nwant\n%s", data, doc)
	}
}

// spec: harness-compilation/command-extension-fields-cannot-select-a-devin-execution-agent
func TestDevinCommandExtensionsCannotSelectExecutionAgent(t *testing.T) {
	command := ir.NewCommand("review", "Review the diff", "Inert command body.\n", map[string]any{
		"allowed-tools": []any{"Read"},
		"agent":         "subagent_general",
		"subagent":      "subagent_general",
	}, ir.SourceRef{File: "commands/review.md", Line: 1})
	result, err := Compile(command, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() || len(result.Artifacts) != 1 {
		t.Fatalf("converted command skill missing: %+v", result)
	}
	fields := devinFrontmatter(t, result.Artifacts[0].Bytes)
	for _, selector := range []string{"agent", "subagent"} {
		if _, present := fields[selector]; present {
			t.Fatalf("extension field selected a Devin execution agent via %q: %#v", selector, fields)
		}
	}
	tools, _ := fields["allowed-tools"].([]any)
	if len(tools) != 1 || tools[0] != "read" {
		t.Fatalf("mapped allowed-tools restriction changed: %#v", fields["allowed-tools"])
	}
	if fields["x-agent-marketplace-kind"] != "command" {
		t.Fatalf("command provenance lost: %#v", fields)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
		t.Fatalf("reserved selectors dropped without a diagnostic: %+v", result.Diagnostics)
	}
}

// spec: harness-compilation/native-devin-skills-keep-execution-selectors
func TestDevinNativeSkillsKeepExecutionSelectors(t *testing.T) {
	const doc = "---\nname: explore\ndescription: Explore the codebase\nsubagent: subagent_explore\nagent: reviewer\n---\nInert skill body.\n"
	src := fstest.MapFS{".devin/skills/explore/SKILL.md": {Data: []byte(doc)}}
	result, err := Convert(src, HarnessDevin, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("native skill refused: %+v", result.Diagnostics)
	}
	data, ok := artifactBytes(result, "skills/explore/SKILL.md")
	if !ok {
		t.Fatalf("native skill missing: %+v", result.Artifacts)
	}
	fields := devinFrontmatter(t, data)
	if fields["subagent"] != "subagent_explore" || fields["agent"] != "reviewer" {
		t.Fatalf("native execution selectors changed: %#v", fields)
	}
	if _, marked := fields["x-agent-marketplace-kind"]; marked {
		t.Fatalf("ordinary skill gained command provenance: %#v", fields)
	}
}
