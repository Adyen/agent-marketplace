package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Adversarial coverage for bead agent-marketplace-pu6b at the package boundary:
// the reversal originally covered Devin only. Factory now has the same explicit
// tool policy, so the remaining targets must not move and Devin's widening cannot
// escape to an enforcing target.

var pu6bSource = ir.SourceRef{File: "skills/demo/SKILL.md", Line: 1}

// TestDevinAndFactoryStoppedFailingClosedOnUnrepresentableTools pins the
// divergence the bead accepted. Cursor, Windsurf and Gemini were never in scope,
// so for the exact inputs Devin and Factory now wave through, every enforcing
// target must behave as it did before: either it represents the tool or it errors
// and emits nothing.
func TestDevinAndFactoryStoppedFailingClosedOnUnrepresentableTools(t *testing.T) {
	unrepresentable := [][]string{
		{"Bash(openspec:*)"},
		{"Read", "Bash(openspec:*)"},
		{"NotATool"},
		{""},
		{"  "},
	}

	for _, target := range Targets() {
		if target == TargetDevin || target == TargetFactory {
			continue
		}
		for _, tools := range unrepresentable {
			sub := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet", tools, "", nil, "", nil, pu6bSource)
			result, err := Compile(sub, target)
			if err != nil {
				t.Fatalf("Compile(subagent, %s): %v", target, err)
			}
			hasError := diag.HasErrors(result.Diagnostics)
			if !hasError {
				// The target represented the tool verbatim; that is its
				// pre-existing behaviour and not something this bead touched.
				continue
			}
			for _, art := range result.Artifacts {
				t.Errorf("%s with tools %v: artifact %s emitted alongside an error; a fail-closed target published anyway",
					target, tools, art.Path)
			}
		}
	}
}

// TestDevinFailClosedRestrictionsAreUntouched keeps the two subagent fields the
// bead explicitly did NOT reverse honest on Devin itself. If a future
// "consistency" edit extends the tool warning to these, a resource that declares
// a restriction Devin has no field for would install unrestricted.
func TestDevinFailClosedRestrictionsAreUntouched(t *testing.T) {
	for _, tc := range []struct {
		name            string
		isolation       string
		disallowedTools []string
	}{
		{"isolation", "full", nil},
		{"disallowedTools", "", []string{"Bash"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := ir.NewSubagent("demo", "Demo subagent", "Body\n", "sonnet",
				[]string{"Read"}, "", tc.disallowedTools, tc.isolation, nil, pu6bSource)
			result, err := Compile(sub, TargetDevin)
			if err != nil {
				t.Fatalf("Compile(subagent, Devin): %v", err)
			}
			if !diag.HasErrors(result.Diagnostics) {
				t.Errorf("diagnostics = %+v, want an error: a restriction Devin has no field for stopped failing closed", result.Diagnostics)
			}
			if len(result.Artifacts) != 0 {
				t.Errorf("artifacts = %d, want 0", len(result.Artifacts))
			}
		})
	}
}

// TestDevinWideningLaundersIntoAnEnforcingTargetOnRoundTrip records the reach of
// the accepted widening, which is wider than "Devin runs with default tools".
//
// Claude → Devin drops the restriction and writes a file with NO allowed-tools
// key. That file is a legitimate conversion SOURCE (`convert --from devin`), and
// ingesting it cannot distinguish "the author wanted no restriction" from "the
// Devin compiler could not spell the restriction the author wrote". So the IR it
// produces has no restriction, and recompiling to Claude — a target that DOES
// enforce allowed-tools — yields an unrestricted Claude skill.
//
// Before the reversal this path did not exist: no Devin artifact was written, so
// there was nothing to ingest. ingest.devin.tool-unreversible does not help,
// because there is no longer an unreversible tool in the file to catch; the
// restriction is simply gone. Nothing here is fixable without reversing the
// decision, so this test pins the exit and names the cost.
func TestDevinWideningLaundersIntoAnEnforcingTargetOnRoundTrip(t *testing.T) {
	original := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, pu6bSource,
		ir.WithSkillAllowedTools([]string{"Bash(git commit:*)"}))

	toDevin, err := Compile(original, TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(toDevin.Artifacts) == 0 {
		t.Fatalf("Compile(skill, Devin) produced no artifacts")
	}
	devinDoc := string(toDevin.Artifacts[0].Bytes)
	if strings.Contains(devinDoc, "allowed-tools") {
		t.Fatalf("precondition changed: the Devin artifact carries a restriction:\n%s", devinDoc)
	}

	src := fstest.MapFS{".devin/skills/demo/SKILL.md": &fstest.MapFile{Data: []byte(devinDoc)}}
	bundle, err := IngestBundle(src, "devin")
	if err != nil {
		t.Fatalf("IngestBundle(devin): %v", err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(bundle.Resources))
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource = %T, want *ir.Skill", bundle.Resources[0])
	}
	if skill.AllowedTools != nil {
		t.Fatalf("ingested AllowedTools = %v; the round trip resurrected a restriction from nowhere", skill.AllowedTools)
	}
	if diag.HasErrors(bundle.Diagnostics) {
		t.Errorf("ingest reported an error for a file the Devin compiler itself wrote: %+v", bundle.Diagnostics)
	}

	backToClaude, err := Compile(skill, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(skill, Claude): %v", err)
	}
	claudeDoc := string(backToClaude.Artifacts[0].Bytes)
	if strings.Contains(claudeDoc, "allowed-tools") {
		t.Fatalf("unexpected: the Claude recompile carries a restriction:\n%s", claudeDoc)
	}
	t.Logf("ACCEPTED LOSS: Claude(Bash(git commit:*)) -> Devin -> IR -> Claude yields an UNRESTRICTED Claude skill:\n%s", claudeDoc)
}

// TestDevinRoundTripPreservesARepresentableRestriction is the control for the
// test above: when Devin CAN spell the restriction, the round trip must not lose
// it, and must not invent one either.
func TestDevinRoundTripPreservesARepresentableRestriction(t *testing.T) {
	original := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, pu6bSource,
		ir.WithSkillAllowedTools([]string{"Read", "Grep"}))
	toDevin, err := Compile(original, TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(toDevin.Artifacts) == 0 {
		t.Fatalf("Compile(skill, Devin) produced no artifacts")
	}
	src := fstest.MapFS{".devin/skills/demo/SKILL.md": &fstest.MapFile{Data: toDevin.Artifacts[0].Bytes}}
	bundle, err := IngestBundle(src, "devin")
	if err != nil {
		t.Fatalf("IngestBundle(devin): %v", err)
	}
	if len(bundle.Resources) == 0 {
		t.Fatalf("IngestBundle(devin) produced no resources")
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource = %T, want *ir.Skill", bundle.Resources[0])
	}
	if got := strings.Join(skill.AllowedTools, ","); got != "read,grep" {
		t.Errorf("AllowedTools = %q, want %q", got, "read,grep")
	}
	if skill.ToolSource != "devin" {
		t.Errorf("ToolSource = %q, want devin", skill.ToolSource)
	}
}

// TestDevinRoundTripPreservesAnExplicitEmptyRestriction is the empty-boundary
// control at the round-trip level: `allowed-tools: []` means "no tools" and must
// survive as an explicit empty list, never decay into an absent key.
func TestDevinRoundTripPreservesAnExplicitEmptyRestriction(t *testing.T) {
	original := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, pu6bSource,
		ir.WithSkillAllowedTools([]string{}))
	toDevin, err := Compile(original, TargetDevin)
	if err != nil {
		t.Fatalf("Compile(skill, Devin): %v", err)
	}
	if len(toDevin.Artifacts) == 0 {
		t.Fatalf("Compile(skill, Devin) produced no artifacts")
	}
	if !strings.Contains(string(toDevin.Artifacts[0].Bytes), "allowed-tools: []") {
		t.Fatalf("explicit empty restriction did not survive compilation:\n%s", toDevin.Artifacts[0].Bytes)
	}
	src := fstest.MapFS{".devin/skills/demo/SKILL.md": &fstest.MapFile{Data: toDevin.Artifacts[0].Bytes}}
	bundle, err := IngestBundle(src, "devin")
	if err != nil {
		t.Fatalf("IngestBundle(devin): %v", err)
	}
	if len(bundle.Resources) == 0 {
		t.Fatalf("IngestBundle(devin) produced no resources")
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource = %T, want *ir.Skill", bundle.Resources[0])
	}
	if skill.AllowedTools == nil {
		t.Error("an explicit empty restriction ingested as ABSENT: \"no tools\" became \"all tools\"")
	}
	if len(skill.AllowedTools) != 0 {
		t.Errorf("AllowedTools = %v, want empty", skill.AllowedTools)
	}
}
