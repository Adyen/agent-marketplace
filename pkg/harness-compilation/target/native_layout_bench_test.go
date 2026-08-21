package target

import "testing"

func BenchmarkLookupNativeLayout(b *testing.B) {
	for b.Loop() {
		_, _ = LookupNativeLayout(TargetCodex)
	}
}
