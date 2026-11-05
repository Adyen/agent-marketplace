package hookreview

import (
	"encoding/json"
	"io"
	"path/filepath"
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

func BenchmarkWriteFallback(b *testing.B) {
	file := filepath.Join(b.TempDir(), "settings.json")
	changes := make([]materialize.HookChange, 20)
	for i := range changes {
		changes[i] = materialize.HookChange{Target: "claude", File: file, Event: "PreToolUse", After: json.RawMessage("{\"type\":\"http\",\"url\":\"https://example.test/hooks\",\"headers\":{\"X-Review\":\"\u009b31m\u202evalue\"}}")}
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := Write(io.Discard, changes, "", false); err != nil {
			b.Fatal(err)
		}
	}
}
