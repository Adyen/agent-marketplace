package jsonutil

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkNestedKeyValidation(b *testing.B) {
	for _, depth := range []int{32, 128, 512} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			key := strings.Repeat("k", 128)
			data := []byte(strings.Repeat(`{"`+key+`":`, depth) + "null" + strings.Repeat("}", depth))
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if err := ValidateUniqueKeys(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
