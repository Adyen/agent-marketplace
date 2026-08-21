package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/registryfs"
)

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const brokenSkillMD = `---
name: %NAME%
description: [unterminated
---

# Broken
`

func brokenSkill(name string) string {
	return strings.Replace(brokenSkillMD, "%NAME%", name, 1)
}

// TestDiscoverClassifiesMalformedResourceKind pins the kind that stops
// cross-kind misattribution: a skill that fails to load must be recorded as a
// skill so an absent command or MCP server of the same name is not blamed on it.
func TestDiscoverClassifiesMalformedResourceKind(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "skills", "valid", "SKILL.md"), "---\nname: valid\ndescription: ok\n---\n\n# Valid\n")
	writeAt(t, filepath.Join(dir, "skills", "broken", "SKILL.md"), brokenSkill("broken"))

	loaded, err := Discover(registryfs.Local(dir), "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(loaded.Malformed) == 0 {
		t.Fatalf("expected a malformed entry, got none")
	}
	var found bool
	for _, m := range loaded.Malformed {
		if m.DirName != "broken" {
			continue
		}
		found = true
		if m.Kind != ir.KindSkill.String() {
			t.Fatalf("expected the malformed entry to be classified as a skill, got %q", m.Kind)
		}
	}
	if !found {
		t.Fatalf("expected a malformed entry for the broken skill, got %#v", loaded.Malformed)
	}

	// The classification must be usable: same kind correlates, other kinds do not.
	if _, ok := skill.MatchMalformedForKind(loaded.Malformed, ir.KindSkill.String(), "broken"); !ok {
		t.Fatalf("a malformed skill must correlate with a skill selection")
	}
	for _, kind := range []ir.ResourceKind{ir.KindCommand, ir.KindMcpServer, ir.KindSubagent, ir.KindCollection} {
		if _, ok := skill.MatchMalformedForKind(loaded.Malformed, kind.String(), "broken"); ok {
			t.Fatalf("a malformed skill must not correlate with a %s selection", kind)
		}
	}
}

// TestDiscoverRootResourceMalformedIsCorrelatable is the production wiring for
// the root-bundle case: the ingester reports the failure with DirName ".", so
// the recovered frontmatter name is the only thing that can identify it.
func TestDiscoverRootResourceMalformedIsCorrelatable(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "SKILL.md"), brokenSkill("root-skill"))

	loaded, err := Discover(registryfs.Local(dir), "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(loaded.Malformed) == 0 {
		t.Fatal("expected a malformed entry for the root resource, got none")
	}
	t.Logf("malformed: %#v", loaded.Malformed)
	if _, ok := skill.MatchMalformedForKind(loaded.Malformed, ir.KindSkill.String(), "root-skill"); !ok {
		t.Fatalf("a malformed root resource must correlate with its declared name, got %#v", loaded.Malformed)
	}
	// It must not correlate with the checkout directory name.
	if _, ok := skill.MatchMalformed(loaded.Malformed, filepath.Base(dir)); ok {
		t.Fatalf("a malformed root resource must not correlate with the checkout directory name")
	}
}
