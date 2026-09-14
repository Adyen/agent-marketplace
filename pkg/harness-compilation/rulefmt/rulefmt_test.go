package rulefmt_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/devin"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/windsurf"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/devin"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/windsurf"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/rulefmt"
)

func TestTriggerModesRoundTripAcrossDevinAndWindsurf(t *testing.T) {
	for _, trigger := range []string{"always_on", "manual", "model_decision", "agent", "glob"} {
		t.Run(trigger, func(t *testing.T) {
			description := "Useful guidance"
			var globs []string
			if trigger == "glob" {
				globs = []string{"**/*.go"}
			}
			rule := ir.NewRule("guidance", description, "# Body\n", globs, trigger == "always_on", nil, ir.SourceRef{})
			rule.Trigger = trigger

			windsurfResult, err := compile.Compile(rule, compile.TargetWindsurf)
			if err != nil {
				t.Fatal(err)
			}
			if len(windsurfResult.Artifacts) != 1 {
				t.Fatalf("Windsurf artifacts = %v", windsurfResult.Artifacts)
			}
			devinBundle, err := devin.LoadBundle(fstest.MapFS{
				".devin/rules/guidance.md": {Data: windsurfResult.Artifacts[0].Bytes},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := devinBundle.Resources[0].(*ir.Rule)
			if got.Trigger != trigger {
				t.Fatalf("trigger = %q, want %q", got.Trigger, trigger)
			}
			if got.Description != description || got.Body != "# Body\n" {
				t.Fatalf("semantic round trip = %#v", got)
			}
			if trigger == "glob" && len(got.Globs) != 1 {
				t.Fatalf("globs = %v", got.Globs)
			}
			devinResult, err := compile.Compile(got, compile.TargetDevin)
			if err != nil {
				t.Fatal(err)
			}
			fromDevin, err := windsurf.LoadRule(fstest.MapFS{
				".windsurf/rules/guidance.md": {Data: devinResult.Artifacts[0].Bytes},
			}, ".windsurf/rules/guidance.md")
			if err != nil {
				t.Fatal(err)
			}
			if fromDevin.Trigger != trigger ||
				fromDevin.Description != description || fromDevin.Body != "# Body\n" {
				t.Fatalf("Devin to Windsurf semantic round trip = %#v", fromDevin)
			}
		})
	}
}

func TestNativeRulePreservesExtraAndDiagnosesIncompatibleGlobs(t *testing.T) {
	rule := ir.NewRule("guidance", "Useful guidance", "# Body\n", []string{"**/*.go"}, false, map[string]any{"x-owner": "platform"}, ir.SourceRef{})
	rule.Trigger = "manual"

	result, err := compile.Compile(rule, compile.TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), "x-owner: platform") {
		t.Fatalf("extra field not preserved: artifacts=%v", result.Artifacts)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeRuleDevinGlobsDropped {
		t.Fatalf("diagnostics = %+v, want %s", result.Diagnostics, diag.CodeRuleDevinGlobsDropped)
	}

	loaded, err := rulefmt.Load(fstest.MapFS{
		".devin/rules/guidance.md": {Data: result.Artifacts[0].Bytes},
	}, ".devin/rules/guidance.md", ".devin/", "devin")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Extra["x-owner"] != "platform" {
		t.Fatalf("loaded extra = %#v", loaded.Extra)
	}
}

func TestLoadRejectsInvalidFilenameDerivedName(t *testing.T) {
	_, err := rulefmt.Load(fstest.MapFS{
		"rules/invalid name.md": {Data: []byte("---\ntrigger: manual\n---\nBody\n")},
	}, "rules/invalid name.md", "", "windsurf")
	if err == nil || !strings.Contains(err.Error(), "resource name") {
		t.Fatalf("Load error = %v, want invalid resource-name error", err)
	}
}
