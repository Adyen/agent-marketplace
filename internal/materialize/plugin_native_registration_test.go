package materialize

import (
	"bytes"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestClaudeNativeCatalogManifestUsesRelativePluginSources(t *testing.T) {
	catalog := nativePluginCatalog{
		name:   "am-example",
		target: target.TargetClaude,
		entries: []nativePluginCatalogEntry{{
			name: "review-kit", generationPath: "/state/packages/generation-1",
		}},
	}

	got, err := catalog.manifest()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
  "name": "am-example",
  "owner": {
    "name": "Agent Marketplace"
  },
  "plugins": [
    {
      "name": "review-kit",
      "source": "./plugins/review-kit"
    }
  ]
}
`)
	if !bytes.Equal(got, want) {
		t.Fatalf("Claude marketplace manifest =\n%s\nwant:\n%s", got, want)
	}
}
