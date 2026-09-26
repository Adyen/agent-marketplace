package claude

import (
	"testing"
	"testing/fstest"
)

// BenchmarkLoadBundlePluginFormWithRootSkill guards the discovery path added
// for single-skill plugin repositories (root SKILL.md plus a plugin manifest).
// Catalog loads run it on every list/status/sync, so a regression that makes
// this path expensive shows up here.
func BenchmarkLoadBundlePluginFormWithRootSkill(b *testing.B) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"humanizer","version":"3.0.0","skills":["./"]}`)},
		"SKILL.md":                   {Data: []byte("---\nname: humanizer\ndescription: Rewrite AI-sounding text\n---\nBody")},
		"agents/openai.yaml":         {Data: []byte("mode: overwrite\n")},
		"README.md":                  {Data: []byte("# Humanizer\n")},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := LoadBundleWithOptions(src, LoadOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
