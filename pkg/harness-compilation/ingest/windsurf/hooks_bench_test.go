package windsurf

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func BenchmarkWindsurfHookIngest(b *testing.B) {
	var text strings.Builder
	text.WriteString(`{"hooks":{"pre_run_command":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			text.WriteByte(',')
		}
		fmt.Fprintf(&text, `{"command":"echo %d"}`, i)
	}
	text.WriteString(`]}}`)
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(text.String())}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := (Ingester{}).LoadBundle(src); err != nil {
			b.Fatal(err)
		}
	}
}
