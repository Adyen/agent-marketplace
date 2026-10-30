package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
)

// spec: catalog-discovery/oversized-skill-frontmatter-is-reported-as-malformed
func TestDiscoverSkillsReportsOversizedFrontmatterAsMalformed(t *testing.T) {
	root := t.TempDir()
	header := "name: big\ndescription: oversized\npad: "
	for name, content := range map[string]string{
		"big":   "---\n" + header + strings.Repeat("x", 64<<10+1-len(header)) + "\n---\nbody\n",
		"small": "---\nname: small\ndescription: ok\n---\nbody\n",
		"edge":  "---\nname: edge\ndescription: at the ceiling\npad: " + strings.Repeat("x", 64<<10-len("name: edge\ndescription: at the ceiling\npad: ")) + "\n---\nbody\n",
	} {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	skills, malformed, err := DiscoverSkills(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	found := map[string]bool{}
	for _, s := range skills {
		found[s.Name] = true
	}
	if !found["small"] || !found["edge"] || found["big"] {
		t.Fatalf("skills = %v, want small and edge only", found)
	}
	if len(malformed) != 1 || malformed[0].DirName != "big" || !errors.Is(malformed[0].Error, bundle.ErrFrontmatterTooLarge) {
		t.Fatalf("malformed = %+v, want big refused with ErrFrontmatterTooLarge", malformed)
	}
}
