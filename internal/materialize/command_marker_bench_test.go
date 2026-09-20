package materialize

import "testing"

func BenchmarkInjectCommandMarker(b *testing.B) {
	for _, sample := range []struct{ name, content string }{
		{"plain", "---\ndescription: sample\n---\nBody\n"},
		{"body-marker", "---\ndescription: sample\n---\n_managed_by: agent-marketplace\n"},
		{"managed", "---\n_managed_by: agent-marketplace\ndescription: sample\n---\nBody\n"},
	} {
		b.Run(sample.name, func(b *testing.B) {
			data := []byte(sample.content)
			b.ReportAllocs()
			for b.Loop() {
				injectManagedByFrontmatter(data)
			}
		})
	}
}
