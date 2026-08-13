package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkParseFrontmatter captures the cost of reading and parsing the
// YAML frontmatter block at the top of a SKILL.md file. The body size
// sub-benchmarks quantify the penalty paid by the current implementation,
// which reads the whole file and strings.Split()s it line-wise even though
// the frontmatter always lives in the first few lines.
func BenchmarkParseFrontmatter(b *testing.B) {
	cases := []struct {
		name    string
		bodyKiB int
		numTags int
	}{
		{"tiny_body_3tags", 0, 3},
		{"medium_body_3tags", 8, 3},
		{"large_body_10tags", 64, 10},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "SKILL.md")
			tags := make([]string, c.numTags)
			for i := range tags {
				tags[i] = fmt.Sprintf("  - tag-%d", i)
			}
			frontmatter := fmt.Sprintf(`---
name: bench-skill
description: Synthetic skill used by BenchmarkParseFrontmatter
metadata:
  tags:
%s
---
`, strings.Join(tags, "\n"))

			body := strings.Repeat("# heading\n\nlorem ipsum dolor sit amet.\n\n", max(1, c.bodyKiB*1024/40))
			if err := os.WriteFile(path, []byte(frontmatter+body), 0o644); err != nil {
				b.Fatalf("write skill: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, _, err := parseFrontmatter(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
