package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-compilation/case-only-name-collisions-are-refused-portably
func TestCaseOnlyNameCollisionsAreRefusedPortably(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/a.toml": {Data: []byte("name = \"Foo\"\ndescription = \"First\"\ndeveloper_instructions = \"First.\"\n")},
		".codex/agents/b.toml": {Data: []byte("name = \"foo\"\ndescription = \"Second\"\ndeveloper_instructions = \"Second.\"\n")},
		".codex/agents/c.toml": {Data: []byte("name = \"bar\"\ndescription = \"Third\"\ndeveloper_instructions = \"Third.\"\n")},
	}
	result, err := Convert(src, HarnessCodex, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, a := range result.Artifacts {
		paths = append(paths, a.Path)
		if strings.EqualFold(a.Path, ".codex/agents/foo.toml") {
			t.Fatalf("case-only collision emitted %s", a.Path)
		}
	}
	if !strings.Contains(strings.Join(paths, "\n"), ".codex/agents/bar.toml") {
		t.Fatalf("unrelated agent was not emitted: %v", paths)
	}
	var conflict *Diagnostic
	for i, d := range result.Diagnostics {
		if d.Code == diag.CodeBundleConflictDuplicateArtifactPath {
			conflict = &result.Diagnostics[i]
		}
	}
	if conflict == nil || !strings.Contains(conflict.Message, "Foo.toml") || !strings.Contains(conflict.Message, "foo.toml") {
		t.Fatalf("missing case-only conflict naming both paths: %+v", result.Diagnostics)
	}
}

func TestCrossKindCaseOnlyCollisionNamesBothPaths(t *testing.T) {
	bundle := &ingest.Bundle{Resources: []ir.Resource{
		ir.NewSkill("Review", "Skill", "Inert.\n", nil, nil, ir.SourceRef{}),
		ir.NewCommand("review", "Command", "Inert.\n", nil, ir.SourceRef{}),
		ir.NewSkill("other", "Other", "Inert.\n", nil, nil, ir.SourceRef{}),
	}}
	result, err := compileBundleFromIR(bundle, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeBundleConflictCrossKindArtifactPath {
			named = strings.Contains(d.Message, "/Review/") && strings.Contains(d.Message, "/review/")
		}
	}
	if !named {
		t.Fatalf("missing %s naming both paths: %+v", diag.CodeBundleConflictCrossKindArtifactPath, result.Diagnostics)
	}
	skipped := map[string]bool{}
	for _, s := range result.Skipped {
		skipped[s.Name] = true
	}
	if !skipped["Review"] || !skipped["review"] || skipped["other"] {
		t.Fatalf("skipped resources = %+v, want Review and review only", result.Skipped)
	}
	var other bool
	for _, a := range result.Artifacts {
		if strings.Contains(strings.ToLower(a.Path), "/review/") {
			t.Fatalf("colliding resource still contributed %s", a.Path)
		}
		other = other || strings.Contains(a.Path, "/other/")
	}
	if !other {
		t.Fatalf("unrelated skill was not emitted: %+v", result.Artifacts)
	}
}

// spec: harness-compilation/unicode-case-folded-paths-collide
func TestUnicodeCaseFoldedPathsCollide(t *testing.T) {
	outputs := []resourceOutput{
		{kind: ir.KindSkill, name: "first", artifacts: []Artifact{{Path: "shared/notes-σ.md"}}},
		{kind: ir.KindSkill, name: "second", artifacts: []Artifact{{Path: "shared/notes-ς.md"}}},
		{kind: ir.KindSkill, name: "third", artifacts: []Artifact{{Path: "shared/other.md"}}},
	}
	artifacts, diagnostics, skipped := resolveArtifactPathConflicts(outputs, TargetClaude)
	if len(artifacts) != 1 || artifacts[0].Path != "shared/other.md" || len(skipped) != 2 {
		t.Fatalf("sigma and final sigma paths were not refused: artifacts=%+v skipped=%+v", artifacts, skipped)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != diag.CodeBundleConflictDuplicateArtifactPath || !strings.Contains(diagnostics[0].Message, "σ") || !strings.Contains(diagnostics[0].Message, "ς") {
		t.Fatalf("missing conflict naming both paths: %+v", diagnostics)
	}
}
