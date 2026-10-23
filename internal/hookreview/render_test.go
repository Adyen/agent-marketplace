package hookreview

import (
	"bytes"
	"encoding/json"
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
