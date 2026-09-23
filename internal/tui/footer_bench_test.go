package tui

import (
	"errors"
	"strings"
	"testing"
)

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
