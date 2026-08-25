package ingest_test

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"

	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/codex"
)

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
