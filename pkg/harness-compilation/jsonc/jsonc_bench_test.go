package jsonc

import (
	"strings"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	small := []byte("{\n  // c\n  \"mcpServers\": {\n    \"docs\": {\"command\": \"x\", \"args\": [\"a\", \"b\"]}\n  }\n}\n")
	var sb strings.Builder
	sb.WriteString(`{"mcpServers":{`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"srv`)
		sb.WriteString(strings.Repeat("x", 3))
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString(string(rune('a' + (i/26)%26)))
		sb.WriteString(`":{"command":"c","args":["a"],"env":{"K":"V"}}`)
	}
	sb.WriteString("}}")
	large := []byte(sb.String())

	for _, tc := range []struct {
		name string
		data []byte
	}{{"small-commented", small}, {"200-servers", large}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Parse("f.json", tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCanonicalize(b *testing.B) {
	data := []byte("{\n  // c\n  \"mcpServers\": {\n    \"docs\": {\"command\": \"x\", \"args\": [\"a\", \"b\"]}\n  }\n}\n")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Canonicalize("f.json", data); err != nil {
			b.Fatal(err)
		}
	}
}
