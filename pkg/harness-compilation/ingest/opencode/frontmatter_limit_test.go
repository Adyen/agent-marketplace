package opencode

import (
	"strings"
	"testing"
)

// spec: harness-compilation/oversized-yaml-frontmatter-is-refused-before-parsing
func TestOversizedFrontmatterReportsCeilingCode(t *testing.T) {
	header := "description: oversized\npad: "
	oversized := "---\n" + header + strings.Repeat("x", 64<<10+1-len(header)) + "\n---\nbody\n"
	_, _, _, err := frontmatter([]byte(oversized))
	if err == nil {
		t.Fatal("oversized OpenCode frontmatter must be refused")
	}
	if d := failure("agents/big.md", err); d.Code != "ingest.frontmatter.too-large" {
		t.Fatalf("diagnostic code = %q, want ingest.frontmatter.too-large", d.Code)
	}
	if d := failure("agents/bad.md", errReadRefused); d.Code == "ingest.frontmatter.too-large" {
		t.Fatal("unrelated failures must keep their OpenCode code")
	}
}
