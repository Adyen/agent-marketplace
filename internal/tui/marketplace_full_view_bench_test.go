package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var marketplaceBundleResult *bundleTree

func BenchmarkMarketplaceSkillBundleForRow(b *testing.B) {
	for _, primary := range []bool{false, true} {
		b.Run(fmt.Sprintf("primary-on-disk-%t", primary), func(b *testing.B) {
			dir := filepath.Join(b.TempDir(), "sha256_"+strings.Repeat("a", 64))
			if err := os.Mkdir(dir, 0o755); err != nil {
				b.Fatal(err)
			}
			body := "# Grill with docs\n\nPrimary instructions with 日本語.\n"
			if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n\nSupporting instructions.\n"), 0o644); err != nil {
				b.Fatal(err)
			}
			if primary {
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			r := installedMarketplaceRow(ir.KindSkill, "grill-with-docs", dir)
			skill := ir.NewSkill("grill-with-docs", "Grill with docs", body, nil, nil, ir.SourceRef{})
			skill.SupportFiles = []string{"notes.md"}
			r.Artifact.Resource = skill
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree, err := bundleTreeForRow(r)
				if err != nil {
					b.Fatal(err)
				}
				marketplaceBundleResult = tree
			}
		})
	}
}
