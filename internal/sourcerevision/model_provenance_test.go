package sourcerevision

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// spec: harness-model-catalogs/legacy-source-revisions-ignore-provenance
func TestModelProvenanceLegacyFallbackDigests(t *testing.T) {
	for _, schema := range []string{"v1", "v2", "v3"} {
		for _, source := range []string{"claude", "factory", "cursor", "devin", "codex"} {
			t.Run(schema+"/"+source, func(t *testing.T) {
				model, effort := "claude-sonnet-4-5-20250929", "high"
				if source == "devin" {
					model, effort = "gemini", ""
				}
				if source == "codex" {
					model, effort = "gpt-5.6-luna", "xhigh"
				}
				r := ir.NewSubagent("review", "Review", "Body", model, nil, effort, nil, "", nil, ir.SourceRef{})
				r.ModelSource = source
				want := ir.NewSubagent("review", "Review", "Body", model, nil, effort, nil, "", nil, ir.SourceRef{})
				switch source {
				case "factory", "cursor":
					want.Model = "sonnet"
				case "devin":
					want.Model = "gemini-3-flash-preview"
				case "codex":
					want.Model, want.Effort = "", ""
					want.Ext.Codex = &ext.Codex{Model: "gpt-5.6-luna", ReasoningEffort: "xhigh"}
				}
				payload, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				contract, err := lockfile.RevisionContract(schema)
				if err != nil {
					t.Fatal(err)
				}
				root := t.TempDir()
				sourceFS, err := fsboundary.OpenSourceFS(root)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := sourceFS.Close(); err != nil {
						t.Fatal(err)
					}
				})
				got, err := buildResource(root, sourceFS, catalog.Entry{Kind: ir.KindSubagent, Name: "review", Resource: r}, contract)
				if err != nil {
					t.Fatal(err)
				}
				if got.ContentDigest != contentDigest(payload) {
					t.Fatalf("legacy digest changed: got %s want %s", got.ContentDigest, contentDigest(payload))
				}
				if r.Model != model || r.Effort != effort || r.ModelSource != source || r.Ext.Codex != nil {
					t.Fatalf("digest projection mutated runtime resource: %+v", r)
				}
			})
		}
	}
}

// spec: harness-model-catalogs/current-source-revision-retains-provenance
func TestModelProvenanceCurrentFallbackDigest(t *testing.T) {
	r := ir.NewSubagent("review", "Review", "Body", "gpt-5.6-luna", nil, "medium", nil, "", nil, ir.SourceRef{})
	r.ModelSource = "codex"
	contract, err := lockfile.RevisionContract("v4")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	sourceFS, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sourceFS.Close(); err != nil {
			t.Fatal(err)
		}
	})
	got, err := buildResource(root, sourceFS, catalog.Entry{Kind: ir.KindSubagent, Name: "review", Resource: r}, contract)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentDigest != contentDigest(payload) {
		t.Fatal("current source revision lost native provenance")
	}
}
