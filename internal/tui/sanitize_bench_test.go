package tui

import (
	"strings"
	"testing"
)

func BenchmarkSanitizeTerminalData(b *testing.B) {
	for _, test := range []struct {
		name  string
		value string
		mode  terminalDataMode
	}{
		{name: "benign", value: "ordinary printable registry metadata", mode: terminalSingleLine},
		{name: "hostile", value: strings.Repeat("safe\x1b]52;c;c2VjcmV0\x07\u202e\n", 32), mode: terminalSingleLine},
		{name: "markdown entities", value: strings.Repeat("body &#27;[2J &#x202E; &Tab; &#4294967323;\n", 32), mode: terminalMarkdownBody},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if output := sanitizeTerminalData(test.value, test.mode); output == "" {
					b.Fatal("empty output")
				}
			}
		})
	}
}
