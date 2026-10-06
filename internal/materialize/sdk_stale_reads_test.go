package materialize

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type sdkStaleReadObserver struct {
	slog.Handler
	mutate func(string, string)
}

func (h sdkStaleReadObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path := "", ""
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "phase" {
			phase = attr.Value.String()
		}
		if attr.Key == "path" {
			path = attr.Value.String()
		}
		return true
	})
	h.mutate(phase, path)
	return h.Handler.Handle(ctx, record)
}

func TestSDKStaleReadsRefuseRetargetedParent(t *testing.T) {
	output, foreign := t.TempDir(), t.TempDir()
	parent := filepath.Join(output, "nested")
	retained := filepath.Join(t.TempDir(), "original-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "stale.txt"), []byte("inert selected stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	foreignFile := filepath.Join(foreign, "stale.txt")
	if err := os.WriteFile(foreignFile, []byte("foreign inert bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreignFile)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-read" {
			return
		}
		changed = true
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("SDK stale discovery did not preserve retargeted parent: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(foreignFile); err != nil || string(raw) != "foreign inert bytes" {
		t.Fatalf("foreign stale bytes changed: %q %v", raw, err)
	}
	if after, err := os.Stat(foreignFile); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign stale metadata changed: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "stale.txt")); err != nil || string(raw) != "inert selected stale payload" {
		t.Fatalf("selected stale payload changed: %q %v", raw, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "nested" {
		t.Fatalf("parent refusal changed output members: %v %v", entries, err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, parent); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil || len(report.Removed) != 1 || report.Removed[0] != "nested/stale.txt" {
		t.Fatalf("healthy nested stale prune failed: %+v %v", report, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
		t.Fatalf("healthy prune left payload/parent/stage: %v %v", entries, err)
	}
}

func TestSDKStaleReadsFIFOReplacementIsBounded(t *testing.T) {
	if os.Getenv("AM_SDK_STALE_FIFO_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSDKStaleReadsFIFOReplacementIsBounded$")
		command.Env = append(os.Environ(), "AM_SDK_STALE_FIFO_CHILD=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("SDK stale quarantine blocked on FIFO: %s", output)
		}
		if err != nil {
			t.Fatalf("SDK stale FIFO child failed: %v\n%s", err, output)
		}
		return
	}
	output, retained := t.TempDir(), filepath.Join(t.TempDir(), "original-stale")
	stale := filepath.Join(output, "stale.txt")
	if err := os.WriteFile(stale, []byte("inert original stale bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	changed := false
	var fifo os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if phase != "sdk-stale-quarantine" {
			return
		}
		if changed {
			return
		}
		changed = true
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		fifo, err = os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if !changed || !errors.Is(err, hcc.ErrStalePruneConflict) {
		t.Fatalf("SDK stale FIFO did not refuse: %+v %v", report, err)
	}
	if after, err := os.Lstat(stale); err != nil || !os.SameFile(fifo, after) || fifo.Mode() != after.Mode() || !fifo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("FIFO generation changed: %v", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert original stale bytes" {
		t.Fatalf("original stale bytes changed: %q %v", raw, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "stale.txt" {
		t.Fatalf("SDK FIFO refusal left quarantine/stage: %v %v", entries, err)
	}
	if err := os.Remove(stale); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, stale); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil || len(report.Removed) != 1 || report.Removed[0] != "stale.txt" {
		t.Fatalf("healthy stale prune did not actually remove: %+v %v", report, err)
	}
}

// spec: materialization/sdk-stale-reads-refuse-multiply-linked-payloads
func TestSDKStaleReadsRefuseMultiplyLinkedPayloads(t *testing.T) {
	output, foreignRoot := t.TempDir(), t.TempDir()
	stale, foreign := filepath.Join(output, "stale.txt"), filepath.Join(foreignRoot, "foreign.txt")
	if err := os.WriteFile(stale, []byte("independently authored inert stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(stale, foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if after, statErr := os.Stat(stale); statErr != nil || !os.SameFile(before, after) {
		t.Fatalf("SDK deleted multiply-linked stale payload: report=%+v err=%v stat=%v", report, err, statErr)
	}
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("SDK stale reader did not refuse unsafe file: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "independently authored inert stale payload" {
		t.Fatalf("foreign bytes changed: %q %v", raw, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign metadata changed: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "stale.txt" {
		t.Fatalf("refusal changed output membership: %v %v", entries, err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil || len(report.StaleCandidates) != 1 || report.StaleCandidates[0].Path != "stale.txt" {
		t.Fatalf("healthy regular stale prune failed: %+v %v", report, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
		t.Fatalf("healthy SDK did not remove actual stale payload/stage: %v %v", entries, err)
	}
}
