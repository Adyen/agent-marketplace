package ingest_test

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"

	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/codex"
)

func BenchmarkBareBundleIngest(b *testing.B) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo\n")},
		"commands/hi.md":       {Data: []byte("---\nname: hi\ndescription: Hi\n---\nHi\n")},
		"mcp.json":             {Data: []byte(`{"mcpServers":{"probe":{"command":"npx","args":["-y","x"]}}}`)},
	}
	harness, ok := ingest.Autodetect(src)
	if !ok {
		b.Fatal("Autodetect did not find Claude bare bundle")
	}
	ing, ok := ingest.For(harness)
	if !ok {
		b.Fatalf("ingester not registered for %q", harness)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ing.LoadBundle(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAutodetect(b *testing.B) {
	tests := []struct {
		name      string
		src       fstest.MapFS
		want      ingest.Harness
		wantFound bool
	}{
		{
			name:      "codex-hit",
			src:       fstest.MapFS{".codex/hooks.json": {Data: []byte(`{"hooks":{}}`)}},
			want:      ingest.HarnessCodex,
			wantFound: true,
		},
		{
			name: "miss",
			src:  fstest.MapFS{},
		},
		{
			name: "ambiguous",
			src: fstest.MapFS{
				".codex/hooks.json":            {Data: []byte(`{"hooks":{}}`)},
				".claude/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\n---\n")},
			},
			want:      ingest.HarnessCodex,
			wantFound: true,
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for b.Loop() {
				got, found := ingest.Autodetect(tt.src)
				if got != tt.want || found != tt.wantFound {
					b.Fatalf("Autodetect = (%q, %v), want (%q, %v)", got, found, tt.want, tt.wantFound)
				}
			}
		})
	}
}
