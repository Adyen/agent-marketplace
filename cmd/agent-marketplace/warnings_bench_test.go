package main

import (
	"fmt"
	"io"
	"testing"
)

func BenchmarkWriteGroupedWarnings(b *testing.B) {
	warnings := make([]error, 100)
	for i := range warnings {
		warnings[i] = testResourceWarning{
			resource: fmt.Sprintf("resource-%03d", i),
			message:  "resource was dropped",
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := writeGroupedWarnings(io.Discard, warnings, false); err != nil {
			b.Fatal(err)
		}
	}
}
