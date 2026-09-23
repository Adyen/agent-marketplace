package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFooterErrorSanitizesUnsafeDataAndKeepsUnicode(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 160, 24
	m.loading = false
	m.errs = []error{errors.New("unsafe \x1b[2J payload ✓")}
	footer := ansi.Strip(renderFooter(m))
	if !strings.Contains(footer, `\x1B[2J`) || !strings.Contains(footer, "✓") {
		t.Fatalf("sanitized footer = %q", footer)
	}
	if strings.Contains(footer, "\x1b") {
		t.Fatalf("raw escape remained in footer = %q", footer)
	}
}
