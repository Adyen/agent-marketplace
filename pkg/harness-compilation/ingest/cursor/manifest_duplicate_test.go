package cursor

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// The registry-supplied plugin manifest feeds ownership markers and output
// paths, so a duplicate "version" must be rejected instead of silently resolving
// to the last value.
func TestLoadBundleRejectsDuplicateKeyInPluginManifest(t *testing.T) {
	src := fstest.MapFS{
		".cursor-plugin/plugin.json": {Data: []byte(`{"name":"demo","version":"` + testutil.FakeSecret + `","version":"` + testutil.FakeSecret + `"}`)},
		"skills/my-skill/SKILL.md":   {Data: []byte("---\nname: my-skill\ndescription: does things\n---\nBody\n")},
	}

	_, err := LoadBundle(src)
	testutil.AssertDuplicateKeyDiagnostic(t, err, ".cursor-plugin/plugin.json", "$", "version")
}
