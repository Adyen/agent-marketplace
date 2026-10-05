package tui

import (
	"bytes"
	"strings"
	"testing"
)

// spec: tui-rendering/editor-running
func TestEditorHandoffShowsWaitingScreenOnAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	p := newEditorProcess(editorHelperCmd(t, "TUI_EDITOR_HELPER_EXIT=0")).
		withWaitingScreen("/home/me/.agents/config.yaml\x1b[2J", "vim")
	p.SetStdout(&out)
	if err := p.Run(); err != nil {
		t.Fatalf("editor run: %v", err)
	}
	got := out.String()
	enter := "\x1b[?1049h"
	if !strings.HasPrefix(got, enter) {
		t.Fatalf("the waiting screen must be drawn on the alternate screen before the editor starts: %q", got)
	}
	if !strings.Contains(got, "/home/me/.agents/config.yaml") || !strings.Contains(got, "vim") {
		t.Fatalf("the waiting screen must name the file and editor: %q", got)
	}
	if strings.Contains(got, "\x1b[2J/") || strings.Count(got, "\x1b[2J") != 2 {
		t.Fatalf("untrusted path bytes must be escaped, and only the handoff clears the screen: %q", got)
	}
	if strings.Contains(got, "\x1b[?1049l") {
		t.Fatalf("the handoff must never reveal the pre-launch primary screen: %q", got)
	}
	if !strings.HasSuffix(got, enter+"\x1b[2J\x1b[H") {
		t.Fatalf("after the editor exits the alternate screen must be re-entered and cleared for the TUI: %q", got)
	}
}

func TestEditorHandoffWithoutWaitingScreenWritesNothing(t *testing.T) {
	var out bytes.Buffer
	p := newEditorProcess(editorHelperCmd(t, "TUI_EDITOR_HELPER_EXIT=0"))
	p.SetStdout(&out)
	if err := p.Run(); err != nil {
		t.Fatalf("editor run: %v", err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("an editor process without a waiting screen must not drive the terminal: %q", out.String())
	}
}
