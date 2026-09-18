package materialize

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func BenchmarkRemoveOwnedMCPEntry(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	all := make([]string, 100)
	for i := range all {
		all[i] = fmt.Sprintf("server-%03d", i)
	}
	remaining := append([]string(nil), all[:50]...)
	remaining = append(remaining, all[51:]...)
	allSelection := devinMcpSelection(all...)
	remainingSelection := devinMcpSelection(remaining...)
	b.Run("sync", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			repo := b.TempDir()
			report, err := Run(context.Background(), repo, allSelection)
			if err != nil || report == nil || len(report.Diags) != 0 {
				b.Fatalf("seed Run = %#v err=%v", report, err)
			}
			b.StartTimer()
			report, err = Run(context.Background(), repo, remainingSelection)
			if err != nil || report == nil || len(report.Diags) != 0 {
				b.Fatalf("sync Run = %#v err=%v", report, err)
			}
			b.StopTimer()
			preview, err := DevinRepairPreview(context.Background(), repo, remainingSelection)
			if err != nil {
				b.Fatal(err)
			}
			file := benchmarkRepairFile(b, preview, harness.DevinMcpConfigFile)
			state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
			if state == nil || len(diags) != 0 {
				b.Fatalf("sync state = %#v diagnostics=%v", state, diags)
			}
			validateCleanupBenchmarkEntries(b, state.existing, remaining)
			b.StartTimer()
		}
	})
	b.Run("targeted", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			repo := b.TempDir()
			report, err := Run(context.Background(), repo, allSelection)
			if err != nil || report == nil || len(report.Diags) != 0 {
				b.Fatalf("seed Run = %#v err=%v", report, err)
			}
			preview, err := DevinRepairPreview(context.Background(), repo, remainingSelection)
			if err != nil {
				b.Fatal(err)
			}
			file := benchmarkRepairFile(b, preview, harness.DevinMcpConfigFile)
			state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
			if state == nil || len(diags) != 0 {
				b.Fatalf("targeted initial state = %#v diagnostics=%v", state, diags)
			}
			var target ownership.EntryValue
			for _, entry := range state.existing {
				if entry.ID.Key == "server-050" {
					target = entry
				}
			}
			b.StartTimer()
			if _, err := file.RemoveOwned(context.Background(), target, true); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			state, diags = file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
			if state == nil || len(diags) != 0 {
				b.Fatalf("targeted state = %#v diagnostics=%v", state, diags)
			}
			validateCleanupBenchmarkEntries(b, state.existing, remaining)
			b.StartTimer()
		}
	})
}

func validateCleanupBenchmarkEntries(tb testing.TB, entries []ownership.EntryValue, remaining []string) {
	tb.Helper()
	if len(entries) != len(remaining) {
		tb.Fatalf("entries = %d, want %d", len(entries), len(remaining))
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		expected := ownership.Digest([]byte(fmt.Sprintf(`{"command":"%s-binary"}`, entry.ID.Key)))
		if entry.ValueDigest != expected {
			tb.Fatalf("entry %q digest = %q, want %q", entry.ID.Key, entry.ValueDigest, expected)
		}
		seen[entry.ID.Key] = true
	}
	if seen["server-050"] {
		tb.Fatal("removed target remains")
	}
	for _, name := range remaining {
		if !seen[name] {
			tb.Fatalf("retained target %q missing", name)
		}
	}
}

func benchmarkRepairFile(tb testing.TB, preview *RepairPreview, rel string) *RepairFile {
	tb.Helper()
	for _, file := range preview.Files {
		if file.Rel == rel {
			return file
		}
	}
	tb.Fatalf("repair file %q missing", rel)
	return nil
}
