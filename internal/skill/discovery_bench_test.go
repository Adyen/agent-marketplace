package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkDiscoverSkills measures the cost of walking a registry directory
// and parsing frontmatter for every SKILL.md it finds. The number of skills
// is the main axis of interest: sync and status paths both call this once
// per registry.
func BenchmarkDiscoverSkills(b *testing.B) {
	cases := []int{20, 100, 500}

	for _, n := range cases {
		b.Run(fmt.Sprintf("%d_skills", n), func(b *testing.B) {
			registryDir := b.TempDir()
			for i := 0; i < n; i++ {
				dir := filepath.Join(registryDir, fmt.Sprintf("skill-%04d", i))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
				content := fmt.Sprintf(`---
name: skill-%04d
description: Synthetic skill used for benchmarking
metadata:
  tags:
    - tag-%d
    - tag-common
---

# Skill %d

%s
`, i, i%7, i, strings.Repeat("lorem ipsum dolor sit amet.\n", 20))
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				skills, malformed, err := DiscoverSkills(registryDir)
				if err != nil {
					b.Fatal(err)
				}
				if len(skills) != n || len(malformed) != 0 {
					b.Fatalf("unexpected discovery: %d skills, %d malformed", len(skills), len(malformed))
				}
			}
		})
	}
}
