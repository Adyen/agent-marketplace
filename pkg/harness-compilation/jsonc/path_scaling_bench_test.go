package jsonc

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkNestedKeyParsing(b *testing.B) {
	for _, depth := range []int{32, 128, 512} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			key := strings.Repeat("k", 128)
			data := []byte(strings.Repeat(`{"`+key+`":`, depth) + "null" + strings.Repeat("}", depth))
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Parse("aggregate.jsonc", data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
