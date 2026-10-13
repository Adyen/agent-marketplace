package materialize

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/sdk-quarantine-reservation-replacement-preserves-foreign-payload
func TestSDKQuarantineReservationReplacementPreservesForeignPayload(t *testing.T) {
	output := t.TempDir()
	retained := filepath.Join(t.TempDir(), "original-reservation")
	stale := filepath.Join(output, "stale.txt")
	if err := os.WriteFile(stale, []byte("inert selected stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	changed := false
	replacement := ""
	var before os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-quarantine" {
			return
		}
		changed = true
		entries, err := os.ReadDir(output)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".agent-marketplace-stale-") {
				replacement = filepath.Join(output, entry.Name())
				break
			}
		}
		if replacement == "" {
			t.Fatal("SDK did not create quarantine reservation")
		}
		if err := os.Rename(replacement, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(replacement, []byte("foreign inert quarantine replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err = os.Stat(replacement)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if raw, readErr := os.ReadFile(replacement); readErr != nil || string(raw) != "foreign inert quarantine replacement" {
		t.Fatalf("SDK consumed quarantine replacement: hit=%t report=%+v err=%v raw=%q read=%v", changed, report, err, raw, readErr)
	}
	if !changed || !errors.Is(err, hcc.ErrStalePruneConflict) {
		t.Fatalf("SDK reservation replacement did not refuse: %+v %v", report, err)
	}
	if after, err := os.Stat(replacement); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("quarantine replacement metadata changed: %v", err)
	}
	if raw, err := os.ReadFile(stale); err != nil || string(raw) != "inert selected stale payload" {
		t.Fatalf("refusal changed selected stale payload: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(retained); err != nil || len(raw) != 0 {
		t.Fatalf("original reservation changed: %q %v", raw, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 2 {
		t.Fatalf("SDK refusal changed output members/stage cleanup: %v %v", entries, err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil {
		t.Fatalf("healthy SDK stale prune failed: %+v %v", report, err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("healthy SDK did not actually prune selected stale: %v", err)
	}
	if raw, err := os.ReadFile(replacement); err != nil || string(raw) != "foreign inert quarantine replacement" {
		t.Fatalf("healthy prune deleted earlier quarantine replacement: %q %v", raw, err)
	}
}
