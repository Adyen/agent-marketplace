package tui

import (
	"errors"
	"strings"
	"testing"
)

func BenchmarkRenderStartupFooter(b *testing.B) {
	m := NewModel("/workspace/repo", "/home/user")
	m.width, m.height = 80, 24
	m.assessmentPending = true
	m.warnings = []error{errors.New("warning")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderFooter(m)
	}
}

// BenchmarkRenderOperationFooter covers the AC5 status bar while an
// operation and its phase, drift and diagnostic segments render together.
func BenchmarkRenderOperationFooter(b *testing.B) {
	m := NewModel("/workspace/repo", "/home/user")
	m.width, m.height = 80, 24
	m.syncing = true
	m.progressLabel = "sync"
	m.operationPhase = "materializing"
	m.progress = &operationProgressSource{operation: 1, updates: make(chan operationProgressMsg, 1)}
	m.warnings = []error{errors.New("warning")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderFooter(m)
	}
}

func BenchmarkRenderErrorFooter(b *testing.B) {
	m := NewModel("/workspace/repo", "/home/user")
	m.width = 50
	m.height = 12
	m.errs = []error{errors.New("config mutation failed: " + strings.Repeat("unsafe/path ", 12))}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderFooter(m)
	}
}
