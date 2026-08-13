package factory

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// The per-skill description/triggers in the registry-supplied manifest feed
// ownership markers and output paths, so a duplicate key nested inside the
// skills array must be rejected instead of silently resolving to the last value.
func TestLoadBundleRejectsDuplicateKeyInPluginManifest(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"demo","skills":[{"name":"my-skill","description":"real","description":"spoofed"}]}`)},
		"skills/my-skill/SKILL.md":    {Data: []byte("Do the thing\n")},
	}

	_, err := New().LoadBundle(src)
	testutil.AssertDuplicateKeyDiagnostic(t, err, ".factory-plugin/plugin.json", "$.skills[0]", "description")
}
