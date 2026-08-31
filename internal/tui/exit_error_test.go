package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The sync-and-quit error (and any program error) is returned to Cobra,
// which prints it to the restored terminal. Untrusted paths inside those
// errors must not carry terminal controls out of the TUI boundary.

func TestSanitizeReturnedErrorNeutralizesUntrustedTerminalBytes(t *testing.T) {
	sentinel := errors.New("sync failed")
	for _, tc := range []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "OSC title in path",
			message: "materialize /cache/\x1b]0;owned\x07/SKILL.md",
			want:    `materialize /cache/\x1B]0;owned\x07/SKILL.md`,
		},
		{
			name:    "CSI styling and erase in registry URL",
			message: "fetch https://example.invalid/\x1b[31mregistry\x1b[2J.git",
			want:    `fetch https://example.invalid/\x1B[31mregistry\x1B[2J.git`,
		},
		{
			name:    "bytes after alternate-screen exit",
			message: "path \x1b[?1049l followed by \x1b[10;10Hfake prompt",
			want:    `path \x1B[?1049l followed by \x1B[10;10Hfake prompt`,
		},
		{
			name:    "structural newline becomes single line",
			message: "failed\nError: spoofed success\r\nnext",
			want:    `failed\x0AError: spoofed success\x0D\x0Anext`,
		},
		{
			name:    "C1 and bidi controls",
			message: "output a\u0085b\u202cend",
			want:    `output a\x85b\u202Cend`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := sanitizeReturnedError(fmt.Errorf("%s: %w", tc.message, sentinel))
			if wrapped == nil {
				t.Fatal("sanitizeReturnedError returned nil for non-nil error")
			}
			got := wrapped.Error()
			if got != tc.want+": "+sentinel.Error() {
				t.Fatalf("sanitized text = %q, want %q", got, tc.want+": "+sentinel.Error())
			}
			if strings.ContainsRune(got, '\x1b') || strings.ContainsAny(got, "\r\n") {
				t.Fatalf("sanitized text still carries raw terminal bytes: %q", got)
			}
		})
	}
}

type exitErrorProbe struct{ detail string }

func (e *exitErrorProbe) Error() string { return "probe: " + e.detail }

func TestSanitizeReturnedErrorPreservesUnwrapIdentity(t *testing.T) {
	sentinel := errors.New("sentinel")

	wrapped := sanitizeReturnedError(fmt.Errorf("outer: %w", sentinel))
	if !errors.Is(wrapped, sentinel) {
		t.Fatalf("errors.Is lost the sentinel through sanitization: %v", wrapped)
	}

	probe := &exitErrorProbe{detail: "typed"}
	typed := sanitizeReturnedError(fmt.Errorf("outer: %w", probe))
	var target *exitErrorProbe
	if !errors.As(typed, &target) || target.detail != "typed" {
		t.Fatalf("errors.As lost the typed identity through sanitization: %v", typed)
	}

	if sanitizeReturnedError(nil) != nil {
		t.Fatal("sanitizeReturnedError(nil) must stay nil")
	}
}

func TestFinishTUIRunSanitizesSyncAndQuitErrorAfterRestoration(t *testing.T) {
	syncSentinel := errors.New("sync backend failed")
	m := Model{syncQuitErr: fmt.Errorf("sync /tmp/\x1b]52;c;c2VjcmV0=\x07/registry: %w", syncSentinel)}

	err := finishTUIRun(m, nil)
	if err == nil {
		t.Fatal("sync-and-quit error was swallowed")
	}
	if !errors.Is(err, syncSentinel) {
		t.Fatalf("sync-and-quit error lost Unwrap identity: %v", err)
	}
	got := err.Error()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("sync-and-quit error carried OSC bytes to Cobra: %q", got)
	}
	if !strings.Contains(got, `\x1B]52;c;c2VjcmV0=\x07`) {
		t.Fatalf("sync-and-quit error did not render readable escapes: %q", got)
	}
}

func TestFinishTUIRunSanitizesReturnedProgramError(t *testing.T) {
	renderer := errors.New("cancel reader /dev/tty\x1b[2J failed")
	err := finishTUIRun(nil, renderer)
	if !errors.Is(err, renderer) {
		t.Fatalf("returned program error lost identity: %v", err)
	}
	if strings.ContainsRune(err.Error(), '\x1b') {
		t.Fatalf("returned program error carried CSI bytes: %q", err.Error())
	}

	// tea sentinel identity must survive sanitization for callers that
	// branch on errors.Is(err, tea.ErrProgramKilled).
	killed := finishTUIRun(nil, tea.ErrProgramKilled)
	if !errors.Is(killed, tea.ErrProgramKilled) {
		t.Fatalf("tea.ErrProgramKilled identity lost: %v", killed)
	}
}
