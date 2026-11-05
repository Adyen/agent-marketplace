package sourcerevision

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCommandToolProvenanceKeepsFrozenFallbackDigest(t *testing.T) {
	plain := ir.NewCommand("deploy", "Deploy", "Inert.\n", map[string]any{"allowed-tools": []any{"Read", "Bash"}}, ir.SourceRef{File: "deploy.md", Line: 1})
	withSource := ir.NewCommand("deploy", "Deploy", "Inert.\n", map[string]any{"allowed-tools": []any{"Read", "Bash"}}, ir.SourceRef{File: "deploy.md", Line: 1})
	withSource.ToolSource = "devin"
	want, err := json.Marshal(legacyHookToolProjection(plain))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(legacyHookToolProjection(withSource))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("command tool provenance changed the fallback digest basis:\n got %s\nwant %s", got, want)
	}
	if withSource.ToolSource != "devin" {
		t.Fatal("projection mutated the ingested command")
	}
}
