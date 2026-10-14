package materialize

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func TestAppendLegacyFallbackAssertionsGuardsCAS(t *testing.T) {
	raw := []byte("{}")
	snapshot := nativeHookAggregateSnapshot{
		abs:        filepath.Join(t.TempDir(), "hooks.json"),
		rel:        ".windsurf/hooks.json",
		raw:        raw,
		generation: dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(raw)},
		mode:       0o640,
	}

	var unmatched nativeHookTransactionExtras
	if err := appendLegacyFallbackAssertions(&unmatched, []nativeHookAggregateSnapshot{snapshot}); err != nil {
		t.Fatalf("unmatched append: %v", err)
	}
	if len(unmatched.assertions) != 1 || !reflect.DeepEqual(unmatched.assertions[0], nativeHookSupportSnapshotAssertion(&snapshot)) {
		t.Fatalf("unmatched assertions = %+v, want the snapshot assertion", unmatched.assertions)
	}
	if assertion := unmatched.assertions[0]; !bytes.Equal(assertion.Bytes, raw) || assertion.Mode != 0o640 || assertion.Delete {
		t.Fatalf("unmatched assertion = %+v, want unchanged raw bytes and mode without delete", assertion)
	}

	matched := nativeHookTransactionExtras{participants: []dependencytxn.DesiredFile{
		{Path: snapshot.abs, Expected: snapshot.generation, ExpectedMode: snapshot.mode},
	}}
	if err := appendLegacyFallbackAssertions(&matched, []nativeHookAggregateSnapshot{snapshot}); err != nil || len(matched.assertions) != 0 {
		t.Fatalf("matched append = %+v, %v; want no duplicate assertion", matched.assertions, err)
	}

	conflict := nativeHookTransactionExtras{assertions: []dependencytxn.DesiredFile{
		{Path: snapshot.abs, Expected: dependencytxn.Generation{}},
	}}
	if err := appendLegacyFallbackAssertions(&conflict, []nativeHookAggregateSnapshot{snapshot}); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("mismatched participant error = %v, want ErrGenerationConflict", err)
	}
}
