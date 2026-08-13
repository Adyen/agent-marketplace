package gemini

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// Gemini's extension manifest is registry-supplied too; the duplicate-key
// failure must be keyed with the same stable code as the other harnesses.
func TestLoadBundleRejectsDuplicateKeyInExtensionManifest(t *testing.T) {
	src := fstest.MapFS{
		"gemini-extension.json":            {Data: []byte(`{"name":"` + testutil.FakeSecret + `","name":"` + testutil.FakeSecret + `","version":"1.0.0"}`)},
		".gemini/skills/my-skill/SKILL.md": {Data: []byte("---\nname: my-skill\ndescription: does things\n---\nBody\n")},
	}

	_, err := LoadBundle(src)
	testutil.AssertDuplicateKeyDiagnostic(t, err, "gemini-extension.json", "$", "name")
}
