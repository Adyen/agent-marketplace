package catalog

import (
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: tui-navigation/local-disk-resources-remain-browsable-outside-their-registry
// spec: tui-navigation/authoritative-remote-inspection-paths-remain-unchanged
func TestSourcePathSelectsAdmittedThenAuthoritativeSources(t *testing.T) {
	root := t.TempDir()
	admitted := filepath.Join(root, ".factory", "skills", "skill", "SKILL.md")
	snapshot := filepath.Join(root, "snapshot", "skills", "skill", "SKILL.md")
	logical := filepath.Join(root, "display", "skills", "skill", "SKILL.md")

	for name, tc := range map[string]struct {
		entry Entry
		want  string
	}{
		"admitted primary wins over other references": {
			entry: Entry{
				Path:              logical,
				PrimarySourcePath: admitted,
				Resource:          ir.NewSkill("skill", "", "", nil, nil, ir.SourceRef{File: snapshot}),
			},
			want: admitted,
		},
		"absolute IR source beats a differing logical entry path": {
			entry: Entry{
				Path:     logical,
				Resource: ir.NewSkill("skill", "", "", nil, nil, ir.SourceRef{File: snapshot}),
			},
			want: snapshot,
		},
		"relative IR source falls back to resolved entry path": {
			entry: Entry{
				Path:     logical,
				Resource: ir.NewSkill("skill", "", "", nil, nil, ir.SourceRef{File: "skills/skill/SKILL.md"}),
			},
			want: logical,
		},
		"nil resource falls back to entry path": {
			entry: Entry{Path: logical},
			want:  logical,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := SourcePath(tc.entry); got != tc.want {
				t.Fatalf("SourcePath = %q, want %q", got, tc.want)
			}
		})
	}
}
