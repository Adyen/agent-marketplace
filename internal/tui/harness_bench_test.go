package tui

import "testing"

func BenchmarkHarnessDialogResizeRender(b *testing.B) {
	for _, target := range []bool{false, true} {
		name := "initialization"
		if target {
			name = "targets"
		}
		b.Run(name, func(b *testing.B) {
			m := NewModel(b.TempDir(), b.TempDir())
			m.loading = false
			m.width, m.height = 80, 30
			if target {
				m.globalHarnesses = []string{"factory", "unknown-existing"}
				m.openHarnessModal()
				m.ensureHarnessForm()
			} else {
				m.pane = paneInitModal
				m.initModal = initModalState{selected: harnessSelection([]string{"factory", "unknown-existing"}), detected: true}
				m.ensureInitForm()
			}
			m.viewContent()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.width, m.height = 50, 12
				m.viewContent()
				m.width, m.height = 80, 30
				m.viewContent()
			}
		})
	}
}
