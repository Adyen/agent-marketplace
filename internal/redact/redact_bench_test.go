package redact

import "testing"

func BenchmarkURL(b *testing.B) {
	for _, sample := range []struct {
		name, value string
	}{
		{"plain", "https://example.invalid/team/repo.git"},
		{"userinfo", "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/team/repo.git"},
		{"query", "https://example.invalid/team/repo.git?token=YOUR_TOKEN_HERE#YOUR_FRAGMENT_HERE"},
		{"diagnostic", "git ls-remote https://example.invalid/team/repo.git?token=YOUR_TOKEN_HERE HEAD: request failed"},
	} {
		b.Run(sample.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if got := URL(sample.value); got == "" {
					b.Fatal("redaction discarded the full diagnostic")
				}
			}
		})
	}
}
