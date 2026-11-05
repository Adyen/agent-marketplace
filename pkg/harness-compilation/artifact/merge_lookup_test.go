package artifact

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMergeJSONCWideObjectPreservesOrderedPatches(t *testing.T) {
	var padding strings.Builder
	for i := 79; i >= 0; i-- {
		fmt.Fprintf(&padding, "    // handwritten %d\n    \"user-%02d\": {\"command\": \"user\", \"number\": 1e0},\n", i, i)
	}
	head := "{\n  // file header\n  \"mcpServers\": {\n" +
		"    // escaped key and handwritten command\n" +
		"    \"\\u0068ead\": {\"command\": \"user\"},\n" + padding.String()
	editBefore := "    // owned edit\n    \"edit\": {\"command\": \"old\", \"_managed_by\": \"agent-marketplace\"},\n"
	editAfter := "    // owned edit\n    \"edit\": {\"command\": \"new\", \"_managed_by\": \"agent-marketplace\"},\n"
	pruned := "    // belongs only to pruned entry\n    \"prune\": {\"command\": \"gone\", \"_managed_by\": \"agent-marketplace\"},\n"
	tail := "    // owned unchanged tail\n    \"tail\": {\"command\": \"same\", \"_managed_by\": \"agent-marketplace\"}"
	end := "\n  },\n  \"handwritten\": 1.00\n}\n"
	existing := head + editBefore + pruned + tail + end
	desired := `{"mcpServers":{"zulu":{"command":"z"},"head":{"command":"must-not-win"},"tail":{"command":"same"},"edit":{"command":"new"},"alpha":{"command":"a"}}}`
	want := head + editAfter + tail +
		",\n    \"alpha\": {\n      \"_managed_by\": \"agent-marketplace\",\n      \"command\": \"a\"\n    }" +
		",\n    \"zulu\": {\n      \"_managed_by\": \"agent-marketplace\",\n      \"command\": \"z\"\n    }" + end
	got, report := mergeOnce(t, existing, desired)
	if string(got) != want {
		t.Fatalf("wide object changed bytes/order/comment association:\n%s\nwant:\n%s", got, want)
	}
	wantReport := ExistingMergeReport{Added: []string{"alpha", "edit", "zulu"}, Removed: []string{"prune"}, Unchanged: []string{"head", "tail"}}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("report=%+v; want %+v", report, wantReport)
	}
	again, _ := mergeOnce(t, string(got), desired)
	if !bytes.Equal(got, again) {
		t.Fatalf("repeat merge rewrote concrete syntax:\n%s", again)
	}
}
