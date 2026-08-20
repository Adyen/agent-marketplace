package tui

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

// Startup/exit boundary benchmarks for the terminal-safety paths. None of
// these existed before the terminal guard, returned-error sanitization, and
// selection-identity work, so they have no base equivalent in
// /tmp/tui-qa-01-before.txt.

// BenchmarkTerminalGuardAcquireRestore measures the per-process startup/
// shutdown cost of the outer terminal guard against a non-terminal output
// (no termios capture), including the trusted restore sequence write.
func BenchmarkTerminalGuardAcquireRestore(b *testing.B) {
	tio := terminalIO{sequenceOut: io.Discard}
	b.ReportAllocs()
	for b.Loop() {
		guard := acquireTerminalGuard(tio)
		guard.restore()
	}
}

// BenchmarkDetectDarkBackgroundFromEnv measures the environment-only
// background selection that replaced the OSC 11 terminal probe.
func BenchmarkDetectDarkBackgroundFromEnv(b *testing.B) {
	b.Setenv("COLORFGBG", "15;0")
	b.ReportAllocs()
	for b.Loop() {
		_ = detectDarkBackgroundFromEnv()
	}
}

// BenchmarkSanitizeReturnedError measures the exit-path sanitization applied
// to errors handed back to Cobra after terminal restoration.
func BenchmarkSanitizeReturnedError(b *testing.B) {
	sentinel := errors.New("sync failed")
	clean := fmt.Errorf("materialize /cache/registry/skills/audit/SKILL.md: %w", sentinel)
	hostile := fmt.Errorf("fetch https://example.invalid/\x1b]52;c;YmVuY2g=\x07/registry\x1b[2J.git\npath: %w", sentinel)
	b.Run("clean", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = sanitizeReturnedError(clean).Error()
		}
	})
	b.Run("hostile", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = sanitizeReturnedError(hostile).Error()
		}
	})
}

// BenchmarkSnapshotRebuildSelectionIdentity measures an accepted snapshot
// rebuild with stable-selection capture and identity restore over a
// realistic list size — the path every reload/refresh/update lands on.
func BenchmarkSnapshotRebuildSelectionIdentity(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprintf("rows_%d", count), func(b *testing.B) {
			m := NewModel("/repo", "/home")
			seedTestTerminalSize(&m)
			m.loading = false
			rows := make([]row, 0, count)
			for i := 0; i < count; i++ {
				rows = append(rows, mkRow(fmt.Sprintf("resource-%04d", i), withGlobal, withRegistry("registry.git", "registry")))
			}
			m.rows = rows
			m.filteredRows = rows
			m.rebuildFlatItems()
			m.cursor = len(m.flatItems) / 2
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				selection := m.selectionKeyForRebuild()
				m.recomputeView()
				m.restoreSelectionAfterRebuild(selection)
			}
		})
	}
}
