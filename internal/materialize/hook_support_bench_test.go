package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkPrepareNativeHookSupport(b *testing.B) {
	var work []skillTarget
	for i := 0; i < 2; i++ {
		root := b.TempDir()
		dir := filepath.Join(root, ".devin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		body := fmt.Sprintf(`{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"echo %d"}]}]}`, i)
		for name, data := range map[string]string{"hooks.v1.json": body, "shared.sh": "echo script", "payload.json": `{"key":"value"}`} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		cat, err := catalog.Load(root, "devin")
		if err != nil || len(cat.Entries) != 1 {
			b.Fatalf("catalog: %v %v", cat, err)
		}
		work = append(work, skillTarget{skill: resolver.ResolvedArtifact{Entry: cat.Entries[0], RegistryURL: root, V4Kind: config.V4SourceDirectory, Source: config.RepoCommitted}, target: hcc.TargetWindsurf})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		prepareHookSupport(work)
	}
}
