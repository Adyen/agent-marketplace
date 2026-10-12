package materialize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkNativeHookReconstructionDisk(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	source := b.TempDir()
	repo := b.TempDir()
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo benchmark"}]}]}}`)
	support := bytes.Repeat([]byte("x"), 1024)
	write := func(file string, data []byte, mode os.FileMode) {
		b.Helper()
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(file, data, mode); err != nil {
			b.Fatal(err)
		}
		if err := os.Chmod(file, mode); err != nil {
			b.Fatal(err)
		}
	}
	write(filepath.Join(source, "hooks", "hooks.json"), definition, 0o644)
	write(filepath.Join(source, "hooks", "run.sh"), support, 0o755)
	loaded, err := catalog.Load(source, "claude")
	if err != nil {
		b.Fatal(err)
	}
	var selected *catalog.Entry
	for i := range loaded.Entries {
		if loaded.Entries[i].Kind == ir.KindHook {
			if selected != nil {
				b.Fatal("benchmark source has multiple hooks")
			}
			selected = &loaded.Entries[i]
		}
	}
	if selected == nil || selected.Name != "PreToolUse-command" {
		b.Fatal("benchmark source does not select the expected hook")
	}
	resolved := resolver.ResolvedArtifact{
		Entry: *selected, RegistryURL: source, CatalogKey: source,
		Source: config.RepoCommitted, V4Kind: config.V4SourceDirectory,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}
	identity := "local:" + filepath.Clean(source)
	digest := sha256.Sum256([]byte(identity))
	sourceKey := filepath.Base(source) + "-" + hex.EncodeToString(digest[:])
	write(filepath.Join(repo, ".factory", "hooks.json"), []byte(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo benchmark"}]}]}`), 0o644)
	write(filepath.Join(repo, ".factory", "hook-support", sourceKey, selected.Name, "run.sh"), support, 0o755)
	ctx := context.Background()
	wantLegacy := os.Getenv("AM_BENCH_EXPECT_LEGACY") == "1"
	run := func() {
		report, err := Run(ctx, repo, []resolver.ResolvedArtifact{resolved}, WithDryRun())
		if err != nil || report == nil {
			b.Fatalf("benchmark public Run failed: %v", err)
		}
		for _, diagnostic := range report.Diags {
			if diagnostic.Severity == diag.SeverityError {
				b.Fatalf("benchmark public Run refused: %s", diagnostic.Code)
			}
		}
		legacy := false
		for _, root := range report.PerRoot {
			for _, written := range root.Written {
				if filepath.Clean(filepath.Join(root.OutDir, written)) == filepath.Join(repo, ".factory", "hooks", "hooks.json") {
					legacy = true
				}
			}
		}
		if legacy != wantLegacy {
			b.Fatalf("benchmark legacy write = %v, want %v", legacy, wantLegacy)
		}
	}
	run()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}
