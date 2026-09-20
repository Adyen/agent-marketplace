package main

import (
	"io"
	"testing"
)

func BenchmarkCLITableEscaping(b *testing.B) {
	for _, sample := range []struct{ name, value string }{{"plain", "registry/control"}, {"controls", "registry\x1b[31mcontrol\t\n\u202e"}} {
		b.Run(sample.name, func(b *testing.B) {
			rows := make([]listRow, 25)
			for i := range rows {
				rows[i] = listRow{Registry: sample.value, Kind: "skill", Name: sample.value, Tags: "tag", Status: "selected"}
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := writeListTable(io.Discard, rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
