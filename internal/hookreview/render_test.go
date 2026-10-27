package hookreview

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func TestWriteEscapesTerminalControls(t *testing.T) {
	changes := []materialize.HookChange{{
		Target: "claude", File: "/repo/\x1b[2J/settings.json", Event: "PreToolUse\nforged", Matcher: "Bash\x1b[2J",
		After: json.RawMessage(`{"hooks":[{"type":"command","command":"echo \u001b[2J"}]}`),
	}}
	var out bytes.Buffer
	if err := Write(&out, changes, "", false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "\x1b") || strings.Contains(text, "PreToolUse\nforged") {
		t.Fatalf("hook review contains unescaped terminal controls: %q", text)
	}
	for _, want := range []string{`/repo/\x1b[2J/settings.json`, `PreToolUse\nforged`, `matcher "Bash\x1b[2J"`, `command: "echo \x1b[2J"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("review lost the escaped value %q: %q", want, text)
		}
	}
}

// spec: hook-sync-diagnostics/fallback-json-preserves-readable-syntax-while-escaping-controls
func TestWriteEscapesFallbackTerminalControls(t *testing.T) {
	handler := json.RawMessage("{\"type\":\"http\",\"url\":\"https://example.test/\u009b31m/\u202evalue\",\"note\":\"quoted \\\"text\\\" and \\\\u009b stays literal café\"}")
	want := `{"note":"quoted \"text\" and \\u009b stays literal café","type":"http","url":"https://example.test/\u009b31m/\u202evalue"}`
	changes := []materialize.HookChange{{
		Target: "claude", File: filepath.Join(t.TempDir(), "settings.json"), Event: "PreToolUse",
		Before: handler, After: append(append([]byte(`{"hooks":[`), handler...), []byte(`]}`)...),
	}}
	for _, pending := range []bool{false, true} {
		var out bytes.Buffer
		if err := Write(&out, changes, "", pending); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if strings.ContainsAny(text, "\u009b\u202e") {
			t.Fatalf("fallback emitted literal terminal controls: %q", text)
		}
		for _, side := range []string{"- ", "+ "} {
			if !strings.Contains(text, side+want) {
				t.Fatalf("fallback changed readable JSON for %q: %q", side, text)
			}
		}
	}
}
