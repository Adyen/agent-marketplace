package harnesscompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestRule_IngestFactory verifies a bare Factory rules directory ingests into
// an ir.Rule.
func TestRule_IngestFactory(t *testing.T) {
	src := fstest.MapFS{
		".factory/rules/typescript.md": {Data: []byte("# TypeScript Rules\nUse interfaces.\n")},
	}
	bundle, err := IngestBundle(src, HarnessFactory)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	var rules int
	for _, r := range bundle.Resources {
		if r.Kind() == ir.KindRule {
			rules++
		}
	}
	if rules != 1 {
		t.Fatalf("expected 1 rule, got %d (resources: %v)", rules, bundle.Resources)
	}
}

// TestRule_ConvertWindsurfToFactory verifies a Windsurf .mdc rule converts to a
// plain-markdown Factory rule (frontmatter stripped) and materializes to
// rules/<name>.md.
func TestRule_ConvertWindsurfToFactory(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/typescript.mdc": {Data: []byte("---\ndescription: TS\nglobs: '*.ts'\nalwaysApply: true\n---\n# TS Rules\nUse interfaces.\n")},
	}

	result, err := Convert(src, HarnessWindsurf, HarnessFactory)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	// globs+alwaysApply present → Factory metadata-dropped diagnostic.
	if !hasDiag(result.Diagnostics, diag.CodeRuleFactoryMetadataDropped) {
		t.Fatalf("expected %q diagnostic, got %v", diag.CodeRuleFactoryMetadataDropped, result.Diagnostics)
	}

	outDir := t.TempDir()
	if _, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite)); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	got := readFile(t, filepath.Join(outDir, "rules", "typescript.md"))
	if strings.HasPrefix(got, "---") {
		t.Errorf("Factory rule must be plain markdown (no frontmatter), got %q", got)
	}
	if !strings.Contains(got, "# TS Rules") {
		t.Errorf("Factory rule missing body, got %q", got)
	}
}

// TestRule_ConvertFactoryToWindsurf verifies a plain-markdown Factory rule
// converts to a Windsurf .mdc file with frontmatter.
func TestRule_ConvertFactoryToWindsurf(t *testing.T) {
	src := fstest.MapFS{
		".factory/rules/typescript.md": {Data: []byte("# TypeScript Rules\nUse interfaces.\n")},
	}

	result, err := Convert(src, HarnessFactory, HarnessWindsurf)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	outDir := t.TempDir()
	if _, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite)); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	got := readFile(t, filepath.Join(outDir, ".windsurf", "rules", "typescript.mdc"))
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("Windsurf rule must be .mdc with frontmatter, got %q", got)
	}
	if !strings.Contains(got, "alwaysApply: false\n") {
		t.Errorf("Windsurf rule missing alwaysApply, got %q", got)
	}
	if !strings.Contains(got, "# TypeScript Rules") {
		t.Errorf("Windsurf rule missing body, got %q", got)
	}
}

// TestRule_ConvertFactoryToClaude verifies Claude emits a no-primitive
// diagnostic and no rule artifact.
func TestRule_ConvertFactoryToClaude(t *testing.T) {
	src := fstest.MapFS{
		".factory/rules/typescript.md": {Data: []byte("# TS\n")},
	}
	result, err := Convert(src, HarnessFactory, HarnessClaude)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !hasDiag(result.Diagnostics, diag.CodeRuleClaudeNoPrimitive) {
		t.Fatalf("expected %q diagnostic, got %v", diag.CodeRuleClaudeNoPrimitive, result.Diagnostics)
	}
	// Artifact carries no resource-kind field, so assert on the exact path a
	// rule artifact would take (rules/<name>.md) rather than a loose "rules/"
	// substring that could match unrelated paths.
	for _, a := range result.Artifacts {
		if a.Path == "rules/typescript.md" {
			t.Errorf("Claude should not emit a rule artifact, got %q", a.Path)
		}
	}
}

func hasDiag(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
