package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Attack surface 1: misattribution through MalformedSelectionNames -------

// TestMatchMalformedRootResourceIsCorrelatedThroughDeclaredName attacks the
// dot-means-diagnostic-code rule. A bundle whose ROOT is the resource is
// reported by the Claude ingester as LoadFailure(..., "root skill", ".", ".",
// err), so DirName is literally "." — which looksLikeDiagnosticCode treats as a
// code and drops on the floor. The frontmatter name is still recovered by the
// loader, and it is the only usable candidate: the bundle root's base names the
// cache checkout, not the resource.
func TestMatchMalformedRootResourceIsCorrelatedThroughDeclaredName(t *testing.T) {
	entries := []MalformedSkill{{
		DirName:      ".",
		Path:         "/cache/registries/example.com/org/pdf-tools@main",
		DeclaredName: "pdf-tools",
		Error:        errors.New("yaml: did not find expected ',' or ']'"),
	}}
	if _, found := MatchMalformed(entries, "pdf-tools"); !found {
		t.Fatalf("a root-level resource that failed to load must correlate through its recovered frontmatter name")
	}
}

// TestMatchMalformedRootResourceDoesNotLeakCheckoutDirectoryName is the guard
// on the fix above: the bundle root's base is the checkout directory (often the
// repository name), never a resource name, so it must not become a candidate.
func TestMatchMalformedRootResourceDoesNotLeakCheckoutDirectoryName(t *testing.T) {
	entries := []MalformedSkill{{
		DirName:      ".",
		Path:         "/cache/registries/example.com/org/pdf-tools@main",
		DeclaredName: "pdf-tools",
		Error:        errors.New("boom"),
	}}
	for _, name := range []string{"pdf-tools@main", "org", "registries", "."} {
		if _, found := MatchMalformed(entries, name); found {
			t.Fatalf("root-resource diagnostic must not correlate with %q", name)
		}
	}
}

// TestMatchMalformedDiagnosticCodeShapesMatchNothing sweeps the diagnostic-code
// shapes that can appear as DirName. None of them names a resource, and every
// one of them carries a bundle-scoped Path whose base collides with plausible
// selection names.
func TestMatchMalformedDiagnosticCodeShapesMatchNothing(t *testing.T) {
	cases := []struct {
		dirName string
		path    string
		blamed  []string
	}{
		{"json.malformed", "/cache/reg/.claude/settings.json", []string{"settings", "json.malformed"}},
		{"json.duplicate-key", "/cache/reg/.mcp.json", []string{"mcp", ".mcp"}},
		{"ownership.state-missing", "/cache/reg", []string{"reg"}},
		{"catalog.discover.sub-bundles-ignored", "/cache/reg/skills", []string{"skills"}},
		{"bundle.conflict.duplicate-artifact-path", "/cache/reg/commands", []string{"commands"}},
		{"ingest.claude.load-skill-failed", "/cache/reg/agents", []string{"agents"}},
	}
	for _, tc := range cases {
		entries := []MalformedSkill{{DirName: tc.dirName, Path: tc.path, Error: errors.New("boom")}}
		for _, name := range tc.blamed {
			if _, found := MatchMalformed(entries, name); found {
				t.Fatalf("diagnostic code %q (path %q) must not correlate with selection %q", tc.dirName, tc.path, name)
			}
		}
	}
}

// TestMatchMalformedDegenerateInputsMatchNothing covers empty and degenerate
// identity fields. None of them may correlate with an absent selection, and
// none of them may panic.
func TestMatchMalformedDegenerateInputsMatchNothing(t *testing.T) {
	entries := []MalformedSkill{
		{},
		{DirName: "", Path: "", DeclaredName: ""},
		{DirName: "   ", Path: "  ", DeclaredName: " "},
		{DirName: "/", Path: "/"},
		{DirName: ".", Path: "."},
		{DirName: "..", Path: ".."},
	}
	for _, name := range []string{"", " ", ".", "/", "..", "skills", "deploy", "reg"} {
		if m, found := MatchMalformed(entries, name); found {
			t.Fatalf("degenerate malformed entry %#v must not correlate with selection %q", m, name)
		}
	}
}

// TestMalformedSelectionNamesWindowsPathProducesNoBogusCandidate keeps a
// Windows-authored diagnostic path from inventing a candidate. filepath.ToSlash
// is a no-op on POSIX, so the whole backslash string is one path component.
func TestMalformedSelectionNamesWindowsPathProducesNoBogusCandidate(t *testing.T) {
	m := MalformedSkill{DirName: "", Path: `C:\cache\reg\skills\broken\SKILL.md`}
	for _, name := range []string{"broken", "SKILL", "skills", "reg", "C:"} {
		if _, found := MatchMalformed([]MalformedSkill{m}, name); found && name != "broken" {
			t.Fatalf("windows-shaped path must not correlate with %q (candidates %v)", name, MalformedSelectionNames(m))
		}
	}
}

// --- Attack surface 2: DeclaredNameFromFile ---------------------------------

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestDeclaredNameFromFileIgnoresBodyNameWhenFenceIsUnterminated is the
// sharpest misattribution vector in name recovery: the scan does not require
// the frontmatter fence to close, so a file whose fence is never terminated
// lets ANY line in the body that starts with "name:" become the recovered name
// — and that name can be a different, genuinely-absent selection.
func TestDeclaredNameFromFileIgnoresBodyNameWhenFenceIsUnterminated(t *testing.T) {
	p := writeTempFile(t, "SKILL.md", `---
description: [unterminated

# How to author a skill

Every skill manifest starts with a frontmatter block:

name: deploy-tools
`)
	if got := DeclaredNameFromFile(p); got != "" {
		t.Fatalf("an unterminated frontmatter fence must not adopt a body line as the declared name, got %q", got)
	}
}

// TestDeclaredNameFromFileRecoversNameFromTerminatedBrokenFrontmatter is the
// load-bearing positive case the guard above must not break: the YAML is
// invalid, but the fence is closed and the name line is readable.
func TestDeclaredNameFromFileRecoversNameFromTerminatedBrokenFrontmatter(t *testing.T) {
	p := writeTempFile(t, "SKILL.md", `---
name: bubble-tea
description: [unterminated
---

# Broken
`)
	if got := DeclaredNameFromFile(p); got != "bubble-tea" {
		t.Fatalf("expected the recovered frontmatter name, got %q", got)
	}
}

func TestDeclaredNameFromFileEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"first name wins", "---\nname: first\nname: second\n---\n", "first"},
		{"tab indented name is nested", "---\ndescription: x\n\tname: nested\n---\n", ""},
		{"space indented name is nested", "---\nmetadata:\n  name: nested\n---\n", ""},
		{"crlf line endings", "---\r\nname: crlf-skill\r\ndescription: x\r\n---\r\n", "crlf-skill"},
		{"bom before fence", "\ufeff---\nname: bom-skill\n---\n", "bom-skill"},
		{"fence only", "---\n", ""},
		{"fence pair only", "---\n---\n", ""},
		{"empty file", "", ""},
		{"no frontmatter", "# Title\nname: victim\n", ""},
		{"fence inside a string on line 1", `title: "--- not a fence"` + "\nname: victim\n", ""},
		{"quoted name", "---\nname: \"quoted-skill\"\n---\n", "quoted-skill"},
		{"binary garbage", "\x00\x01\x02---\nname: victim\n---\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTempFile(t, "SKILL.md", tc.content)
			if got := DeclaredNameFromFile(p); got != tc.want {
				t.Fatalf("DeclaredNameFromFile = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeclaredNameFromFileBeyondLineCapIsNotRecovered pins the 512-line bound.
func TestDeclaredNameFromFileBeyondLineCapIsNotRecovered(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\n")
	for i := 0; i < maxFrontmatterLines; i++ {
		b.WriteString("filler: x\n")
	}
	b.WriteString("name: too-late\n---\n")
	p := writeTempFile(t, "SKILL.md", b.String())
	if got := DeclaredNameFromFile(p); got != "" {
		t.Fatalf("a name past the frontmatter line cap must not be recovered, got %q", got)
	}
}

// TestDeclaredNameFromFileDirectoryAndMissingFile covers the non-regular-file
// shapes: a directory named SKILL.md, and a directory argument with no manifest.
func TestDeclaredNameFromFileDirectoryAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "SKILL.md"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := DeclaredNameFromFile(dir); got != "" {
		t.Fatalf("a directory named SKILL.md must not yield a name, got %q", got)
	}
	if got := DeclaredNameFromFile(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("a missing file must not yield a name, got %q", got)
	}
}

// TestDeclaredNameFromFileHugeSingleLineIsRejected keeps a pathological file
// (no newlines) from being mistaken for frontmatter.
func TestDeclaredNameFromFileHugeSingleLineIsRejected(t *testing.T) {
	p := writeTempFile(t, "SKILL.md", "---\n"+strings.Repeat("a", 128*1024)+"\nname: victim\n---\n")
	if got := DeclaredNameFromFile(p); got != "" {
		t.Fatalf("a line past the scanner buffer must abort recovery, got %q", got)
	}
}
