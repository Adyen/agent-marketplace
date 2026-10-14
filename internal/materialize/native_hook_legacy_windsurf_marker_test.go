package materialize

import (
	"fmt"
	"strings"
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func windsurfLegacyFile() ownedJSONFileSpec {
	return ownedJSONFileSpec{rel: ".devin/hooks.json", container: "hooks", hookShape: targetmeta.HookShapeEventHandlers, legacyMarkedFlatHooks: true}
}

func TestWindsurfLegacyCommandOnlyMarkerRecognition(t *testing.T) {
	marker := fmt.Sprintf(`"%s":"%s"`, managedByField, managedByValue)
	manual := `{"command":"echo d76-manual-windsurf"}`

	t.Run("marked command-only handler is removed and manual kept", func(t *testing.T) {
		owned := `{"command":"echo owned",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"pre_run_command":[`+manual+`,`+owned+`]}}`)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, windsurfLegacyFile())
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if len(removals) != 1 || removals[0].event != "pre_run_command" {
			t.Fatalf("removals = %+v, want the owned handler under pre_run_command", removals)
		}
		got := string(doc.Bytes())
		if strings.Contains(got, "echo owned") || !strings.Contains(got, manual) {
			t.Fatalf("patched document = %s", got)
		}
	})

	t.Run("documented optional fields are recognized", func(t *testing.T) {
		owned := `{"command":"echo optional","powershell":"pwsh","show_output":true,"working_directory":"x",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"pre_run_command":[`+owned+`]}}`)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, windsurfLegacyFile())
		if err != nil || len(removals) != 1 {
			t.Fatalf("removals = %+v, err = %v; want the optional-field handler removed", removals, err)
		}
	})

	t.Run("marked handler with a type field is refused", func(t *testing.T) {
		owned := `{"type":"command","command":"echo typed",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"pre_run_command":[`+owned+`]}}`)
		if _, err := nativeHookLegacyMarkedFlatPatch(doc, windsurfLegacyFile()); err == nil {
			t.Fatal("marked Windsurf handler with a type field was accepted")
		}
	})

	t.Run("marked handler with a mistyped show_output is refused", func(t *testing.T) {
		owned := `{"command":"echo bad","show_output":"yes",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"pre_run_command":[`+owned+`]}}`)
		if _, err := nativeHookLegacyMarkedFlatPatch(doc, windsurfLegacyFile()); err == nil {
			t.Fatal("marked Windsurf handler with a mistyped show_output was accepted")
		}
	})

	t.Run("unmarked handler is untouched", func(t *testing.T) {
		body := `{"hooks":{"pre_run_command":[` + manual + `]}}`
		doc := geminiLegacyGroupDoc(t, body)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, windsurfLegacyFile())
		if err != nil || len(removals) != 0 {
			t.Fatalf("removals = %+v, err = %v; want none", removals, err)
		}
		if string(doc.Bytes()) != body {
			t.Fatalf("unmarked handler changed: %s", doc.Bytes())
		}
	})
}
