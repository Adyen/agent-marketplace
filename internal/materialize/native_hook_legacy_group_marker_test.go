package materialize

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func geminiLegacyGroupDoc(t *testing.T, body string) *jsonc.Document {
	t.Helper()
	doc, err := jsonc.Parse("settings.json", []byte(body))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc
}

func geminiLegacyGroupFile() ownedJSONFileSpec {
	return ownedJSONFileSpec{rel: "settings.json", container: "hooks", hookShape: targetmeta.HookShapeMatcherGroups, legacyMarkedFlatHooks: true}
}

func TestGeminiLegacyGroupMarkerRecognition(t *testing.T) {
	marker := fmt.Sprintf(`"%s":"%s"`, managedByField, managedByValue)
	owned := `{"matcher":"^run$","hooks":[{"type":"command","command":"echo owned"}],` + marker + `}`
	keep := `{"matcher":"^keep$","hooks":[{"type":"command","command":"echo keep"}]}`
	ownedBeforeTool := map[string]map[string]bool{"BeforeTool": {"echo owned": true}}

	t.Run("group marker removes the owned group only", func(t *testing.T) {
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"BeforeTool":[`+owned+`,`+keep+`]}}`)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), ownedBeforeTool)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if len(removals) != 1 || removals[0].event != "BeforeTool" || removals[0].matcher != "^run$" {
			t.Fatalf("removals = %+v, want the owned group under BeforeTool", removals)
		}
		got := string(doc.Bytes())
		if strings.Contains(got, "echo owned") || !strings.Contains(got, "echo keep") {
			t.Fatalf("patched document = %s", got)
		}
	})

	t.Run("handler marker keeps unmarked handlers", func(t *testing.T) {
		handler := `{"type":"command","command":"echo marked-handler",` + marker + `}`
		unmarked := `{"type":"command","command":"echo keep-handler"}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"BeforeTool":[{"matcher":"^run$","hooks":[`+handler+`,`+unmarked+`]}]}}`)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), map[string]map[string]bool{"BeforeTool": {"echo marked-handler": true}})
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if len(removals) != 1 || removals[0].event != "BeforeTool" {
			t.Fatalf("removals = %+v, want the one marked handler", removals)
		}
		got := string(doc.Bytes())
		if strings.Contains(got, "echo marked-handler") || !strings.Contains(got, "echo keep-handler") || !strings.Contains(got, `"matcher":"^run$"`) {
			t.Fatalf("patched document = %s", got)
		}
	})

	t.Run("emptied marked group is removed", func(t *testing.T) {
		handler := `{"type":"command","command":"echo only",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"BeforeTool":[{"matcher":"^run$","hooks":[`+handler+`]}]}}`)
		if _, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), map[string]map[string]bool{"BeforeTool": {"echo only": true}}); err != nil {
			t.Fatalf("patch: %v", err)
		}
		if strings.Contains(string(doc.Bytes()), "BeforeTool") {
			t.Fatalf("emptied group left its event behind: %s", doc.Bytes())
		}
	})

	t.Run("marked group with an unmatched command is preserved", func(t *testing.T) {
		body := `{"hooks":{"BeforeTool":[{"matcher":"^run$","hooks":[{"type":"command","command":"echo other"}],` + marker + `}]}}`
		doc := geminiLegacyGroupDoc(t, body)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), ownedBeforeTool)
		if err != nil || len(removals) != 0 {
			t.Fatalf("removals = %+v, err = %v; want the group preserved", removals, err)
		}
		if string(doc.Bytes()) != body {
			t.Fatalf("unmatched marked group changed: %s", doc.Bytes())
		}
	})

	t.Run("nil expectation preserves marked groups", func(t *testing.T) {
		body := `{"hooks":{"BeforeTool":[` + owned + `]}}`
		doc := geminiLegacyGroupDoc(t, body)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile())
		if err != nil || len(removals) != 0 {
			t.Fatalf("removals = %+v, err = %v; want none without an expectation", removals, err)
		}
		if string(doc.Bytes()) != body {
			t.Fatalf("marked group changed without an expectation: %s", doc.Bytes())
		}
	})

	t.Run("malformed matched handler is refused", func(t *testing.T) {
		malformed := `{"type":"prompt","command":"echo prompt"}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"BeforeTool":[{"matcher":"^run$","hooks":[`+malformed+`],`+marker+`}]}}`)
		if _, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), map[string]map[string]bool{"BeforeTool": {"echo prompt": true}}); err == nil {
			t.Fatal("matched group with a malformed handler was accepted")
		}
	})

	t.Run("unknown marked handler key is refused", func(t *testing.T) {
		handler := `{"type":"command","command":"echo prompt","prompt":"x",` + marker + `}`
		doc := geminiLegacyGroupDoc(t, `{"hooks":{"BeforeTool":[{"matcher":"^run$","hooks":[`+handler+`]}]}}`)
		if _, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), map[string]map[string]bool{"BeforeTool": {"echo prompt": true}}); err == nil {
			t.Fatal("marked handler with an unrecognized key was accepted")
		}
	})

	t.Run("unmarked group is untouched", func(t *testing.T) {
		body := `{"hooks":{"BeforeTool":[` + keep + `]}}`
		doc := geminiLegacyGroupDoc(t, body)
		removals, err := nativeHookLegacyMarkedFlatPatch(doc, geminiLegacyGroupFile(), ownedBeforeTool)
		if err != nil || len(removals) != 0 {
			t.Fatalf("removals = %+v, err = %v; want none", removals, err)
		}
		if string(doc.Bytes()) != body {
			t.Fatalf("unmarked group changed: %s", doc.Bytes())
		}
	})
}
