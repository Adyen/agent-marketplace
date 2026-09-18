package pipeline

import "testing"

func BenchmarkResolveFromURL(b *testing.B) {
	for b.Loop() {
		ResolveFromURL("https://gitlab.example.com/group/nested/project/-/tree/release/v1/skills/a", "release/v1", "")
	}
}
