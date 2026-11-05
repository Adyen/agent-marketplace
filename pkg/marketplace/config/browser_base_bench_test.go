package config

import "testing"

func BenchmarkValidateBrowserBase(b *testing.B) {
	for _, tc := range []struct {
		name string
		base string
	}{
		{"root", "https://marketplace.test"},
		{"subpath", "https://marketplace.test/tools"},
		{"encoded", "https://marketplace.test/my%20market"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = validateBrowserBase(tc.base) // Measure both accepted and rejected bases.
			}
		})
	}
}
