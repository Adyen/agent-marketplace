package hookreview

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func BenchmarkWrite(b *testing.B) {
	changes := make([]materialize.HookChange, 20)
	for i := range changes {
		changes[i] = materialize.HookChange{Target: "claude", File: "/repo/.claude/settings.json", Event: "PreToolUse", Matcher: "Bash", After: json.RawMessage(`{"hooks":[{"type":"command","command":"echo review"}]}`)}
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := Write(io.Discard, changes, "/home/user", false); err != nil {
			b.Fatal(err)
		}
	}
}
