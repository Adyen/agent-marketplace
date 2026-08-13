package claude

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// The registry-supplied plugin manifest feeds ownership markers and output
// paths, so a duplicate "name" must be rejected instead of silently resolving to
// the last value.
func TestLoadBundleRejectsDuplicateKeyInPluginManifest(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"` + testutil.FakeSecret + `","name":"` + testutil.FakeSecret + `","version":"1.0.0"}`)},
		"skills/my-skill/SKILL.md":   {Data: []byte("---\nname: my-skill\ndescription: does things\n---\nBody\n")},
	}

	_, err := (&claudeIngester{}).LoadBundle(src)
	testutil.AssertDuplicateKeyDiagnostic(t, err, ".claude-plugin/plugin.json", "$", "name")
}

// A present-but-malformed manifest used to be swallowed, yielding an empty
// manifest. It must now fail closed.
func TestLoadBundleRejectsMalformedPluginManifest(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":`)},
		"skills/my-skill/SKILL.md":   {Data: []byte("---\nname: my-skill\ndescription: does things\n---\nBody\n")},
	}

	_, err := (&claudeIngester{}).LoadBundle(src)
	if err == nil {
		t.Fatal("LoadBundle silently accepted a malformed plugin manifest")
	}
	if !strings.Contains(err.Error(), ".claude-plugin/plugin.json") {
		t.Fatalf("error %q does not name the manifest", err)
	}
}
