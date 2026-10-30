package claude

import (
	"strings"
	"testing"
	"testing/fstest"
)

// spec: harness-compilation/oversized-yaml-frontmatter-is-refused-before-parsing
func TestLoadBundleRefusesOversizedFrontmatterAndKeepsSiblings(t *testing.T) {
	header := "name: big\ndescription: oversized\npad: "
	oversized := "---\n" + header + strings.Repeat("x", 64<<10+1-len(header)) + "\n---\nbody\n"
	src := fstest.MapFS{
		"skills/big/SKILL.md":   {Data: []byte(oversized)},
		"skills/small/SKILL.md": {Data: []byte("---\nname: small\ndescription: ok\n---\nbody\n")},
	}
	b, err := LoadBundleWithOptions(src, LoadOptions{})
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	names := map[string]bool{}
	for _, r := range b.Resources {
		names[r.Name()] = true
	}
	if !names["small"] || names["big"] {
		t.Fatalf("resources = %v, want only the sibling skill", names)
	}
	refused := false
	for _, d := range b.Diagnostics {
		if d.Code == "ingest.frontmatter.too-large" && d.Resource == "skills/big" {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("diagnostics = %+v, want ingest.frontmatter.too-large for skills/big", b.Diagnostics)
	}
}
