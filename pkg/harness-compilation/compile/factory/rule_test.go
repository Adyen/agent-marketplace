package factory

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestFactoryRule_PlainMarkdown(t *testing.T) {
	c := &factoryCompiler{}
	rule := ir.NewRule("typescript", "", "# TypeScript Rules\nUse interfaces.\n", nil, false, nil, ir.SourceRef{File: "rules/typescript.md", Line: 1})

	arts, diags, err := c.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for plain rule, got %v", diags)
	}

	art := findArtifactByPath(arts, "rules/typescript.md")
	if art == nil {
		t.Fatalf("missing artifact rules/typescript.md; got %v", arts)
		return
	}
	if got := string(art.Bytes); got != "# TypeScript Rules\nUse interfaces.\n" {
		t.Errorf("factory rule body should be plain markdown, got %q", got)
	}
	// Factory rules must not carry YAML frontmatter.
	if len(art.Bytes) >= 3 && string(art.Bytes[:3]) == "---" {
		t.Errorf("factory rule must not have frontmatter, got %q", string(art.Bytes))
	}
	if art.Fragment {
		t.Error("factory rule artifact should not be a fragment")
	}
}

func TestFactoryRule_MetadataDropped(t *testing.T) {
	c := &factoryCompiler{}
	rule := ir.NewRule("typescript", "TS", "body\n", []string{"*.ts"}, true, nil, ir.SourceRef{File: "rules/typescript.mdc", Line: 1})

	arts, diags, err := c.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if !hasDiagnostic(diags, diag.CodeRuleFactoryMetadataDropped) {
		t.Fatalf("expected %q diagnostic when globs/alwaysApply present, got %v", diag.CodeRuleFactoryMetadataDropped, diags)
	}
	art := findArtifactByPath(arts, "rules/typescript.md")
	if art == nil {
		t.Fatal("missing artifact rules/typescript.md")
		return
	}
	if string(art.Bytes) != "body\n" {
		t.Errorf("expected plain body verbatim, got %q", string(art.Bytes))
	}
}

func TestFactoryRule_DescriptionDropped(t *testing.T) {
	c := &factoryCompiler{}
	rule := ir.NewRule("typescript", "TS conventions", "body\n", nil, false, nil, ir.SourceRef{File: "rules/typescript.mdc", Line: 1})

	_, diags, err := c.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if !hasDiagnostic(diags, diag.CodeRuleFactoryMetadataDropped) {
		t.Fatalf("expected %q diagnostic when description present, got %v", diag.CodeRuleFactoryMetadataDropped, diags)
	}
}
