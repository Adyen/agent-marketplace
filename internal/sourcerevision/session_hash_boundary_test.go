package sourcerevision

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestSessionRejectsMutationBetweenIngestionAndHash(t *testing.T) {
	snapshot := t.TempDir()
	writeFixture(t, filepath.Join(snapshot, ".factory", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\ninitial\n")
	ctx, closeSession := WithSession(context.Background())
	defer func() {
		if err := closeSession(); err != nil {
			t.Errorf("close source-revision session: %v", err)
		}
	}()
	result, err := BuildWithContext(ctx, Input{
		SnapshotRoot:  snapshot,
		RootPath:      ".",
		SourceHarness: "factory",
		beforeHash: func(root string) {
			writeFixture(t, filepath.Join(root, ".factory", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\nchanged after ingest\n")
		},
	})
	if !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("BuildWithContext error = %v, want generation conflict", err)
	}
	if !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("changed source produced a result: %+v", result)
	}
}
