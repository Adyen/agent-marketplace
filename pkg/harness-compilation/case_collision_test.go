package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
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
