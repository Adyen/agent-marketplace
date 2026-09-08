package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// BenchmarkCaptureAggregateDetailDiskIO includes bounded local disk reads for a
// refresh containing more maximum-sized entries than either aggregate budget.
func BenchmarkCaptureAggregateDetailDiskIO(b *testing.B) {
	path := filepath.Join(b.TempDir(), "maximum.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxDetailSourceBytes+1)), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(maxDetailCaptureBytes)
	b.ResetTimer()
	for b.Loop() {
		var budget detailCaptureBudget
		for i := 0; i < maxDetailCaptureEntries+16; i++ {
			_ = budget.capture(path)
		}
		if budget.bytes > maxDetailCaptureBytes || budget.entries > maxDetailCaptureEntries {
			b.Fatal("capture exceeded aggregate budget")
		}
	}
}

func BenchmarkBundlePagerDispatch(b *testing.B) {
	m := bundlePagerTestModel(b)
	b.Setenv("PAGER", os.Args[0])
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, cmd := m.openBundlePager(); cmd == nil {
			b.Fatal("no pager command")
		}
	}
}

func BenchmarkBundleBrowserView(b *testing.B) {
	m := bundlePagerTestModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if m.View() == "" {
			b.Fatal("empty browser")
		}
	}
}

func BenchmarkInlineBundleSelectionDispatch(b *testing.B) {
	m := modelWithSelectedRow(b, inlineBundleRow(b, "alpha"))
	m.rows = append(m.rows, inlineBundleRow(b, "beta"))
	m.recomputeView()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, direction := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
			updated, cmd := m.Update(tea.KeyMsg{Type: direction})
			m = updated.(Model)
			if cmd == nil {
				b.Fatal("selection did not dispatch a bundle load")
			}
		}
	}
}

func BenchmarkInlineBundleFileNavigationDispatch(b *testing.B) {
	m := modelWithSelectedRow(b, inlineBundleRow(b, "alpha"))
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = applyBundleCommand(b, updated.(Model), cmd)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, direction := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
			updated, cmd := m.Update(tea.KeyMsg{Type: direction})
			m = updated.(Model)
			if cmd == nil {
				b.Fatal("navigation did not dispatch a preview")
			}
		}
	}
}

func BenchmarkBundleSourcePreview(b *testing.B) {
	root := b.TempDir()
	content := strings.Repeat("def example(value):\n    if value:\n        return value + 1\n\n", 40)
	if err := os.WriteFile(filepath.Join(root, "script.py"), []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if output := renderBundleFile(root, "script.py", 80); output == "" {
			b.Fatal("empty preview")
		}
	}
}

func BenchmarkCaptureSnapshotTraversalDiskIO(b *testing.B) {
	for _, tc := range []struct {
		name  string
		count int
		size  int
	}{
		{"entries", 40, 32},
		{"bytes", 5, 256 << 10},
	} {
		b.Run(tc.name, func(b *testing.B) {
			resolved, catalogs := captureTraversalFixture(b, tc.count, tc.size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if sources := captureSnapshotDetailSources(resolved, catalogs); len(sources) != 2*tc.count+5 {
					b.Fatalf("captured %d source identities", len(sources))
				}
			}
		})
	}
}

func BenchmarkRenderBoundedDetail(b *testing.B) {
	path := filepath.Join(b.TempDir(), "detail.md")
	content := "# Detail\n\n" + strings.Repeat("content ", 200)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
	r := mkRow("detail", withProject)
	r.Artifact.Path = path
	r.DetailSource = captureDetailSource(path)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if output := renderDetailFullContent(r, 80); output == "" {
			b.Fatal("empty detail")
		}
	}
}
