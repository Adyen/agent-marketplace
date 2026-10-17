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

	for name, pair := range map[string][2]ir.Resource{"claude-controls": {plainSubagent, claudeSubagent}, "factory-category": {legacyDroid, categoryDroid}} {
		want, err := json.Marshal(legacyHookToolProjection(pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(legacyHookToolProjection(pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s changed the fallback digest basis:\n got %s\nwant %s", name, got, want)
		}
	}
	if claudeSubagent.Ext.Claude == nil || categoryDroid.Ext.Factory == nil {
		t.Fatal("projection mutated the ingested resource")
	}
}
