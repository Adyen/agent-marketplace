package skill

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMatchMalformedCoversEveryWayASelectionCanNameADroppedResource pins the
// matching contract both the install path and the sync path depend on. An
// ingester that fails to load a resource attaches either the resource directory
// or the resource file to its diagnostic, and the authored frontmatter `name:`
// can differ from both — bundle.parseSkillFromFS prefers it over the directory
// name. A selection must correlate in all of those shapes, or it is reported as
// an upstream deletion instead of a parse error.
func TestMatchMalformedCoversEveryWayASelectionCanNameADroppedResource(t *testing.T) {
	tests := []struct {
		name      string
		malformed MalformedSkill
		selection string
		wantMatch bool
	}{
		{
			name:      "directory name attached to the diagnostic",
			malformed: MalformedSkill{DirName: "broken-skill", Path: "/cache/reg/skills/broken-skill"},
			selection: "broken-skill",
			wantMatch: true,
		},
		{
			name:      "skill manifest attached instead of its directory",
			malformed: MalformedSkill{Path: "/cache/reg/skills/broken-skill/SKILL.md"},
			selection: "broken-skill",
			wantMatch: true,
		},
		{
			name:      "authored name differs from the directory name",
			malformed: MalformedSkill{DirName: "bubbletea-tui", DeclaredName: "bubble-tea", Path: "/cache/reg/skills/bubbletea-tui"},
			selection: "bubble-tea",
			wantMatch: true,
		},
		{
			name:      "directory name still matches when an authored name exists",
			malformed: MalformedSkill{DirName: "bubbletea-tui", DeclaredName: "bubble-tea", Path: "/cache/reg/skills/bubbletea-tui"},
			selection: "bubbletea-tui",
			wantMatch: true,
		},
		{
			name:      "file-shaped resource name matches through its stem",
			malformed: MalformedSkill{DirName: "deploy.md", Path: "/cache/reg/commands/deploy.md"},
			selection: "deploy",
			wantMatch: true,
		},
		{
			name:      "Cursor rule file name matches through its stem",
			malformed: MalformedSkill{DirName: "secure-coding.mdc", Kind: "rule", Path: "/cache/reg/rules/secure-coding.mdc"},
			selection: "secure-coding",
			wantMatch: true,
		},
		{
			name:      "unrelated selection does not match",
			malformed: MalformedSkill{DirName: "broken-skill", Path: "/cache/reg/skills/broken-skill"},
			selection: "beads",
			wantMatch: false,
		},
		{
			name:      "bundle-level diagnostic matches nothing",
			malformed: MalformedSkill{DirName: "catalog.discover.sub-bundles-ignored", Path: "/cache/reg/skills"},
			selection: "skills",
			wantMatch: false,
		},
		{
			name:      "empty selection matches nothing",
			malformed: MalformedSkill{DirName: "broken-skill", Path: "/cache/reg/skills/broken-skill"},
			selection: "   ",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := MatchMalformed([]MalformedSkill{tc.malformed}, tc.selection)
			if got != tc.wantMatch {
				t.Fatalf("MatchMalformed(%q) = %v, want %v (candidates: %v)",
					tc.selection, got, tc.wantMatch, MalformedSelectionNames(tc.malformed))
			}
		})
	}
}

func TestMalformedSourceFileNamesTheFileToOpen(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/cache/reg/skills/broken-skill", want: filepath.Join("/cache/reg/skills/broken-skill", "SKILL.md")},
		{path: "/cache/reg/skills/broken-skill/SKILL.md", want: "/cache/reg/skills/broken-skill/SKILL.md"},
		{path: "/cache/reg/commands/deploy.md", want: "/cache/reg/commands/deploy.md"},
		{path: "/cache/reg/rules/secure-coding.mdc", want: "/cache/reg/rules/secure-coding.mdc"},
		{path: "", want: ""},
	}
	for _, tc := range tests {
		if got := MalformedSourceFile(MalformedSkill{Path: tc.path}); got != tc.want {
			t.Fatalf("MalformedSourceFile(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestParseFailureNamesFileWithoutSwallowingTheError(t *testing.T) {
	m := MalformedSkill{Path: "/cache/reg/skills/broken-skill", Error: errors.New("yaml: boom")}
	got := ParseFailure(m)
	want := "failed to parse " + filepath.Join("/cache/reg/skills/broken-skill", "SKILL.md")
	if got != want {
		t.Fatalf("ParseFailure() = %q, want %q", got, want)
	}
	// The underlying error must stay out so callers can wrap it with %w.
	if ParseFailureForFile("") != "failed to parse" {
		t.Fatalf("ParseFailureForFile(\"\") = %q", ParseFailureForFile(""))
	}
}

// TestDeclaredNameFromFileRecoversNameFromUnparsableFrontmatter is the point of
// the helper: the YAML does not parse, which is exactly when the declared name
// is needed to correlate a selection with the dropped resource.
func TestDeclaredNameFromFileRecoversNameFromUnparsableFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "bubbletea-tui")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(manifest, []byte("---\nname: bubble-tea\ndescription: [unterminated\nmetadata:\n  name: not-the-resource-name\n---\n\n# Bubble\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both a directory and the manifest itself resolve to the same name.
	for _, path := range []string{skillDir, manifest} {
		if got := DeclaredNameFromFile(path); got != "bubble-tea" {
			t.Fatalf("DeclaredNameFromFile(%q) = %q, want %q", path, got, "bubble-tea")
		}
	}

	if got := DeclaredNameFromFile(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("expected empty declared name for a missing file, got %q", got)
	}

	noFrontmatter := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(noFrontmatter, []byte("# Just a document\nname: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DeclaredNameFromFile(noFrontmatter); got != "" {
		t.Fatalf("expected empty declared name without a frontmatter block, got %q", got)
	}
}
