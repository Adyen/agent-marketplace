package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/convertsync"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// BenchmarkPlanConvertSync measures the ownership classification pass that
// convert --sync runs before it writes anything. The pass reads and digests
// every prune candidate and every colliding destination, so its cost scales
// with the size of the output tree rather than with the size of the conversion.
func BenchmarkPlanConvertSync(b *testing.B) {
	for _, size := range []int{16, 128} {
		b.Run(fmt.Sprintf("output-files=%d", size*2), func(b *testing.B) {
			b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
			srcDir := b.TempDir()
			outDir := filepath.Join(b.TempDir(), "out")
			benchWriteClaudePlugin(b, srcDir, size)

			result, err := hcc.Convert(os.DirFS(srcDir), hcc.Harness("claude"), hcc.Harness("factory"), hcc.WithPluginRoot(srcDir))
			if err != nil {
				b.Fatalf("convert: %v", err)
			}
			report, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite))
			if err != nil {
				b.Fatalf("materialize: %v", err)
			}
			if err := recordConvertOutput(outDir, "factory", report, convertsync.Record{}); err != nil {
				b.Fatalf("record: %v", err)
			}
			// Half the candidates are owned and half are unrelated, so both the
			// prune branch and the preserve branch are exercised.
			for i := 0; i < size; i++ {
				path := filepath.Join(outDir, "docs", fmt.Sprintf("unrelated-%d.md", i))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(fmt.Sprintf("unrelated %d\n", i)), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			record := loadConvertRecord(outDir, "factory")

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				decision, err := planConvertSync(result, outDir, record)
				if err != nil {
					b.Fatalf("plan: %v", err)
				}
				if len(decision.preserved()) != size {
					b.Fatalf("preserved = %d, want %d", len(decision.preserved()), size)
				}
			}
		})
	}
}

// BenchmarkOpaqueConvertDestinations measures the pre-plan readability probe
// convert --sync runs over every artifact destination. It exists to keep that
// probe a per-destination open rather than a second full read of the output tree:
// the plan already reads and digests the destinations it classifies.
func BenchmarkOpaqueConvertDestinations(b *testing.B) {
	const skills = 128
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	srcDir := b.TempDir()
	outDir := filepath.Join(b.TempDir(), "out")
	benchWriteClaudePlugin(b, srcDir, skills)

	result, err := hcc.Convert(os.DirFS(srcDir), hcc.Harness("claude"), hcc.Harness("factory"), hcc.WithPluginRoot(srcDir))
	if err != nil {
		b.Fatalf("convert: %v", err)
	}
	if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
		b.Fatalf("materialize: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opaque, err := opaqueConvertDestinations(result, outDir)
		if err != nil {
			b.Fatalf("scan: %v", err)
		}
		if len(opaque) != 0 {
			b.Fatalf("opaque = %d, want 0 for readable destinations", len(opaque))
		}
	}
}

// BenchmarkRecordConvertOutput measures the post-write bookkeeping: digesting
// every file this run materialized plus re-verifying the prior entries the run
// could not reconcile and therefore carries forward.
func BenchmarkRecordConvertOutput(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	const skills = 128
	srcDir := b.TempDir()
	outDir := filepath.Join(b.TempDir(), "out")
	benchWriteClaudePlugin(b, srcDir, skills)

	result, err := hcc.Convert(os.DirFS(srcDir), hcc.Harness("claude"), hcc.Harness("factory"), hcc.WithPluginRoot(srcDir))
	if err != nil {
		b.Fatalf("convert: %v", err)
	}
	report, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil {
		b.Fatalf("materialize: %v", err)
	}

	// The prior record also owns files this run does not produce, so every
	// benchmark iteration pays the carry-over re-verification too.
	prior := make([]convertsync.OwnedFile, 0, skills)
	for i := 0; i < skills; i++ {
		rel := fmt.Sprintf("legacy/output-%d.md", i)
		body := []byte(fmt.Sprintf("legacy %d\n", i))
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			b.Fatal(err)
		}
		prior = append(prior, convertsync.OwnedFile{Path: rel, Digest: convertsync.DigestOf(body)})
	}
	if err := convertsync.Save(outDir, "factory", prior); err != nil {
		b.Fatalf("save: %v", err)
	}
	priorRecord := loadConvertRecord(outDir, "factory")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := recordConvertOutput(outDir, "factory", report, priorRecord); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
}

// BenchmarkConvertRecordLoad measures reading the durable record, which every
// convert --sync pays once before classification.
func BenchmarkConvertRecordLoad(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	outDir := b.TempDir()
	files := make([]convertsync.OwnedFile, 0, 256)
	for i := 0; i < 256; i++ {
		files = append(files, convertsync.OwnedFile{
			Path:   fmt.Sprintf("skills/skill-%d/SKILL.md", i),
			Digest: convertsync.DigestOf([]byte(fmt.Sprintf("body %d", i))),
		})
	}
	if err := convertsync.Save(outDir, "factory", files); err != nil {
		b.Fatalf("save: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := convertsync.Load(outDir, "factory"); err != nil {
			b.Fatalf("load: %v", err)
		}
	}
}

func benchWriteClaudePlugin(b *testing.B, root string, skills int) {
	b.Helper()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	write(filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"bench-plugin","version":"1.0.0"}`)
	for i := 0; i < skills; i++ {
		name := fmt.Sprintf("bench-skill-%d", i)
		write(filepath.Join(root, "skills", name, "SKILL.md"), "---\nname: "+name+"\ndescription: Bench skill\n---\n\n# "+name+"\n")
	}
}
