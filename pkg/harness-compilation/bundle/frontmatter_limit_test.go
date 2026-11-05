package bundle

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// frontmatterOfSize returns a document whose YAML frontmatter block is exactly
// size bytes, followed by a body.
func frontmatterOfSize(size int) string {
	header := "name: big\ndescription: oversized\npad: "
	return "---\n" + header + strings.Repeat("x", size-len(header)) + "\n---\nbody\n"
}

// spec: harness-compilation/oversized-yaml-frontmatter-is-refused-before-parsing
func TestLoadRefusesYAMLFrontmatterAboveCeiling(t *testing.T) {
	const ceiling = 64 << 10
	loaders := map[string]func(fstest.MapFS) error{
		"skill":    func(src fstest.MapFS) error { _, err := LoadSkill(src, "skills/big"); return err },
		"command":  func(src fstest.MapFS) error { _, err := LoadCommand(src, "commands/big.md"); return err },
		"subagent": func(src fstest.MapFS) error { _, err := LoadSubagent(src, "agents/big.md"); return err },
		// Rules fall back to a lenient parser after strict YAML fails; the
		// ceiling must refuse instead of falling back.
		"rule": func(src fstest.MapFS) error { _, err := LoadRule(src, "rules/big.mdc"); return err },
	}
	for kind, load := range loaders {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				size    int
				refused bool
			}{{ceiling, false}, {ceiling + 1, true}} {
				doc := []byte(frontmatterOfSize(tc.size))
				src := fstest.MapFS{
					"skills/big/SKILL.md": {Data: doc},
					"commands/big.md":     {Data: doc},
					"agents/big.md":       {Data: doc},
					"rules/big.mdc":       {Data: doc},
				}
				err := load(src)
				if !tc.refused {
					if err != nil {
						t.Fatalf("frontmatter at the %d-byte ceiling must load: %v", tc.size, err)
					}
					continue
				}
				coded, ok := diag.AsCodedError(err)
				if !ok || coded.Code != "ingest.frontmatter.too-large" {
					t.Fatalf("frontmatter of %d bytes: got %v, want ingest.frontmatter.too-large", tc.size, err)
				}
			}
		})
	}
}
