package sourcerevision

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestNativeMetadataSidecarsKeepFrozenFallbackDigest(t *testing.T) {
	controls := map[string]any{"permissionMode": "plan"}
	plainSubagent := ir.NewSubagent("helper", "Helper", "Help.\n", "", nil, "", nil, "", controls, ir.SourceRef{File: "agents/helper.md", Line: 1})
	claudeSubagent := ir.NewSubagent("helper", "Helper", "Help.\n", "", nil, "", nil, "", controls, ir.SourceRef{File: "agents/helper.md", Line: 1})
	claudeSubagent.Ext.Claude = &ext.Claude{NativeControls: map[string]any{"permissionMode": "plan"}}

	legacyDroid := ir.NewSubagent("auditor", "Auditor", "Audit.\n", "", []string{"read-only"}, "", nil, "", nil, ir.SourceRef{File: "droids/auditor.md", Line: 1})
	categoryDroid := ir.NewSubagent("auditor", "Auditor", "Audit.\n", "", nil, "", nil, "", nil, ir.SourceRef{File: "droids/auditor.md", Line: 1})
	categoryDroid.Ext.Factory = &ext.Factory{ToolsCategory: "read-only"}

	for _, tc := range []struct {
		name          string
		legacy, typed ir.Resource
	}{
		{"claude-controls", plainSubagent, claudeSubagent},
		{"factory-category", legacyDroid, categoryDroid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rawLegacy, err := json.Marshal(tc.legacy)
			if err != nil {
				t.Fatal(err)
			}
			rawTyped, err := json.Marshal(tc.typed)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(rawLegacy, rawTyped) {
				t.Fatalf("fixture does not exercise the sidecar: both serialize to %s", rawTyped)
			}
			want, err := json.Marshal(legacyHookToolProjection(tc.legacy))
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(legacyHookToolProjection(tc.typed))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("sidecar changed the fallback digest basis:\n got %s\nwant %s", got, want)
			}
		})
	}
	if claudeSubagent.Ext.Claude == nil || categoryDroid.Ext.Factory == nil {
		t.Fatal("projection mutated the ingested resource")
	}
}
