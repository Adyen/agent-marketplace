package config

import "testing"

func BenchmarkValidateRegistryURLs(b *testing.B) {
	entries := []RegistryEntry{
		{URL: "https://gitlab.example.com/team/repo.git", Ref: "main"},
		{URL: "git@gitlab.example.com:team/repo.git", Ref: "main"},
		{URL: "https://github.com/org/repo.git", Ref: "main"},
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = ValidateRegistryURLs(entries, DefaultAllowedRegistries)
	}
}

func BenchmarkNormalizeRegistryURL(b *testing.B) {
	u := "git@gitlab.example.com:team/repo.git"
	b.ReportAllocs()
	for b.Loop() {
		_ = normalizeRegistryURL(u)
	}
}
