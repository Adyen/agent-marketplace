package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: doctor-repair-planning/dependency-journal-recovery-runs-with-invalid-configuration
func TestDoctorRepairDependencyJournal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, after string
		wantErr     bool
	}{
		{name: "configuration remains invalid", after: "apiVersion: agent-marketplace/v4\nharnesses: [\n", wantErr: true},
		{name: "journal repairs configuration", after: "apiVersion: agent-marketplace/v4\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			_, journal := createDoctorDependencyJournal(t, configPath,
				"apiVersion: agent-marketplace/v4\nharnesses: [\n", tc.after)

			var out bytes.Buffer
			err := runDoctor(doctorOptions{
				out: &out, repair: true, yes: true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(nil),
			})
			if tc.wantErr != (err != nil) {
				t.Fatalf("repair error = %v, wantErr %v\n%s", err, tc.wantErr, out.String())
			}
			if !strings.Contains(out.String(), "  1. [safe] replay interrupted dependency journal") ||
				!strings.Contains(out.String(), journal) || !strings.Contains(out.String(), "needs: journal") {
				t.Fatalf("first plan step did not name safe journal replay:\n%s", out.String())
			}
			if tc.wantErr && (!strings.Contains(out.String(), "CONFIGURATION ERRORS") || !strings.Contains(out.String(), "blocked by")) {
				t.Fatalf("configuration errors or blocked selection steps were omitted:\n%s", out.String())
			}
			if !strings.Contains(out.String(), "recovered dependency journal") {
				t.Fatalf("successful replay was not reported:\n%s", out.String())
			}
			if got, readErr := os.ReadFile(configPath); readErr != nil || string(got) != tc.after {
				t.Fatalf("configuration = %q, %v; want %q", got, readErr, tc.after)
			}
			if _, statErr := os.Lstat(journal); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("dependency journal remains: %v", statErr)
			}
			var check bytes.Buffer
			if checkErr := runSyncCheck(context.Background(), &check, false); errors.Is(checkErr, dependencytxn.ErrRecoveryRequired) {
				t.Fatalf("sync --check still reports pending recovery: %v\n%s", checkErr, check.String())
			}
		})
	}
}

// spec: doctor-repair-planning/ownership-journal-recovery-reports-its-outcome
func TestDoctorRepairOwnershipJournal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [\n")
	var fixtures []ownershipJournalFixture
	for _, machine := range []bool{false, true} {
		destinations, err := harness.ResolveDestinations(hcc.TargetDevin, machine, repoRoot)
		if err != nil || len(destinations) == 0 {
			t.Fatalf("resolve Devin destinations (machine=%v): %v", machine, err)
		}
		fixtures = append(fixtures, createDoctorOwnershipJournal(t, destinations[0].Root))
	}

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out: &out, repair: true, yes: true,
		probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err == nil {
		t.Fatal("repair unexpectedly ignored the invalid configuration")
	}
	if !strings.Contains(out.String(), "  1. [safe] recover ownership journal") || !strings.Contains(out.String(), "needs: journal") {
		t.Fatalf("ownership replay was not the first safe stage-1 step:\n%s", out.String())
	}
	for _, fixture := range fixtures {
		if _, err := os.Lstat(fixture.store.JournalFile()); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ownership journal remains at %s: %v", fixture.store.JournalFile(), err)
		}
		if got, err := os.ReadFile(fixture.target); err != nil || !bytes.Equal(got, fixture.next) {
			t.Errorf("recovered aggregate %s = %q, %v", fixture.target, got, err)
		}
		if _, err := os.Stat(fixture.store.StateFile()); err != nil {
			t.Errorf("recovery did not finalize ownership state %s: %v", fixture.store.StateFile(), err)
		}
		if !strings.Contains(out.String(), fixture.store.JournalFile()) {
			t.Errorf("plan omitted ownership journal %s:\n%s", fixture.store.JournalFile(), out.String())
		}
	}
	for _, value := range []string{"generation=1", "action=rolled-forward", "file=hooks.v1.json"} {
		if !strings.Contains(out.String(), value) {
			t.Errorf("recovery outcome omitted %q:\n%s", value, out.String())
		}
	}
	if strings.Contains(out.String(), "aggregate-secret") {
		t.Fatalf("recovery output leaked aggregate bytes:\n%s", out.String())
	}
}

// spec: doctor-repair-planning/refused-journal-recovery-preserves-evidence-and-target-bytes
func TestDoctorRepairJournalRefusal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []string{"foreign target bytes", "non-prefix publication"} {
		t.Run(tc, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			writeFile(t, configPath, "apiVersion: agent-marketplace/v4\n")
			stateDir, err := dependencytxn.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			var journal string
			var targets []string
			if tc == "foreign target bytes" {
				_, journal = createDoctorDependencyJournal(t, lockPath, "old-lock\n", "new-lock\n")
				targets = []string{lockPath}
				if err := os.WriteFile(lockPath, []byte("foreign bytes\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, configPath, "old-config\n")
				second := filepath.Join(repoRoot, ".agents", "other.yaml")
				writeFile(t, second, "old-other\n")
				firstGeneration, err := dependencytxn.ReadGeneration(configPath)
				if err != nil {
					t.Fatal(err)
				}
				secondGeneration, err := dependencytxn.ReadGeneration(second)
				if err != nil {
					t.Fatal(err)
				}
				injected := errors.New("interrupt before publication")
				_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{
					{Path: configPath, Expected: firstGeneration, Bytes: []byte("new-config\n"), Mode: 0o600},
					{Path: second, Expected: secondGeneration, Bytes: []byte("new-other\n"), Mode: 0o600},
				}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
					if point.Stage == dependencytxn.FaultAfterJournal {
						return injected
					}
					return nil
				}))
				if !errors.Is(err, injected) {
					t.Fatalf("create non-prefix journal: %v", err)
				}
				if err := os.WriteFile(second, []byte("new-other\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				targets = []string{configPath, second}
				journals, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
				if err != nil || len(journals) != 1 {
					t.Fatalf("dependency journals = %v, %v", journals, err)
				}
				journal = journals[0]
			}
			if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
				t.Fatalf("pending journal precondition: %v", err)
			}
			snapshot := snapshotDoctorJournalFiles(t, append(targets, journal)...)

			output, err := executeCommand("doctor", "--repair", "--yes")
			if err == nil || !errors.Is(err, dependencytxn.ErrRecoveryBlocked) {
				t.Fatalf("doctor error = %v, want ErrRecoveryBlocked\n%s", err, output)
			}
			snapshot()
		})
	}
}

// spec: doctor-repair-planning/pending-journals-are-previewed-off-terminal-without-consent
func TestDoctorRepairJournalPreview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	_, dependencyJournal := createDoctorDependencyJournal(t,
		filepath.Join(repoRoot, ".agents", "lock.yaml"), "", "version: 2\n")
	destinations, err := harness.ResolveDestinations(hcc.TargetDevin, false, repoRoot)
	if err != nil || len(destinations) == 0 {
		t.Fatalf("resolve Devin repository destination: %v", err)
	}
	owned := createDoctorOwnershipJournal(t, destinations[0].Root)
	snapshot := snapshotDoctorJournalFiles(t,
		dependencyJournal, filepath.Join(repoRoot, ".agents", "lock.yaml"),
		owned.store.JournalFile(), owned.target, owned.store.StateFile())

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out: &out, repair: true, in: strings.NewReader("y\n"),
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(nil),
	})
	if err != nil {
		t.Fatalf("preview pending journals: %v\n%s", err, out.String())
	}
	for _, expected := range []string{
		"  1. [safe] replay interrupted dependency journal",
		"[safe] recover ownership journal",
		"preview only: not a terminal",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("preview omitted %q:\n%s", expected, out.String())
		}
	}
	snapshot()
}

type ownershipJournalFixture struct {
	store  *ownership.Store
	target string
	next   []byte
}

func createDoctorOwnershipJournal(t *testing.T, root string) ownershipJournalFixture {
	t.Helper()
	target := filepath.Join(root, "hooks.v1.json")
	prior := []byte("{\"hooks\":{}}\n")
	next := []byte("{\"hooks\":{\"PreToolUse\":[\"aggregate-secret\"]}}\n")
	writeFile(t, target, string(prior))
	store, err := ownership.Open(root, ownership.WithCrasher(ownership.CrashAt(ownership.CrashAfterAggregateRename)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Apply(ownership.Mutation{
		Rel: "hooks.v1.json", AbsFile: target, NextBytes: next,
		ExpectPriorDigest: ownership.Digest(prior), Reason: ownership.ReasonSync,
	})
	if !errors.Is(err, ownership.ErrInjectedCrash) {
		t.Fatalf("create interrupted ownership transaction: %v", err)
	}
	if _, err := os.Lstat(store.JournalFile()); err != nil {
		t.Fatalf("ownership journal was not created: %v", err)
	}
	return ownershipJournalFixture{store: store, target: target, next: next}
}

func createDoctorDependencyJournal(t *testing.T, target, before, after string) (string, string) {
	t.Helper()
	if before != "" {
		writeFile(t, target, before)
	} else if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	expected, err := dependencytxn.ReadGeneration(target)
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("interrupt after journal")
	_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{{
		Path: target, Expected: expected, Bytes: []byte(after), Mode: 0o600,
	}}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
		if point.Stage == dependencytxn.FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("create interrupted dependency transaction: %v", err)
	}
	journals, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 {
		t.Fatalf("pending dependency journals = %v, want one", journals)
	}
	return stateDir, journals[0]
}

func snapshotDoctorJournalFiles(t *testing.T, paths ...string) func() {
	t.Helper()
	type snapshot struct {
		data []byte
		info os.FileInfo
	}
	before := make(map[string]snapshot, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			before[path] = snapshot{}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = snapshot{data: data, info: info}
	}
	return func() {
		t.Helper()
		for path, prior := range before {
			info, err := os.Lstat(path)
			if prior.info == nil {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("doctor created %s: %v", path, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("doctor removed %s: %v", path, err)
				continue
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(prior.data, data) || !os.SameFile(prior.info, info) ||
				prior.info.Mode() != info.Mode() || !prior.info.ModTime().Equal(info.ModTime()) ||
				prior.info.Size() != info.Size() {
				t.Errorf("doctor changed %s: read=%v bytes=%q mode=%v->%v", path, readErr, data, prior.info.Mode(), info.Mode())
			}
		}
	}
}

// spec: doctor-repair-planning/failed-resolution-blocks-selection-dependent-steps
// spec: doctor-repair-planning/pending-journals-are-previewed-off-terminal-without-consent
func TestDoctorRepairJournalResolutionFailure(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name string
		yes  bool
	}{
		{name: "preview"},
		{name: "replay", yes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n")
			mcpPath, _ := prepareDevinRepairFixture(t, repoRoot)
			store, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			_, journal := createDoctorDependencyJournal(t, lockPath, "old-lock\n", "new-lock\n")
			preserveSelection := snapshotDoctorJournalFiles(t, mcpPath, store.StateFile())
			preserveJournal := snapshotDoctorJournalFiles(t, journal, lockPath)
			resolutionErr := errors.New("test resolution unavailable")
			var out bytes.Buffer
			err = runDoctor(doctorOptions{
				out: &out, repair: true, yes: tc.yes,
				probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: func(context.Context, string) ([]resolver.ResolvedArtifact, error) {
					return nil, resolutionErr
				},
			})
			if !errors.Is(err, resolutionErr) {
				t.Fatalf("repair error = %v, want resolution failure\n%s", err, out.String())
			}
			for _, expected := range []string{
				"  1. [safe] replay interrupted dependency journal",
				"needs: journal",
				resolutionErr.Error(),
				"blocked by",
				"sync harness drift",
				"reconcile shared-aggregate ownership",
			} {
				if !strings.Contains(out.String(), expected) {
					t.Errorf("repair output omitted %q:\n%s", expected, out.String())
				}
			}
			preserveSelection()
			if tc.yes {
				if !strings.Contains(out.String(), "recovered dependency journal") {
					t.Fatalf("authorized replay was not reported:\n%s", out.String())
				}
				if got, readErr := os.ReadFile(lockPath); readErr != nil || string(got) != "new-lock\n" {
					t.Fatalf("replayed lock = %q, %v", got, readErr)
				}
				if _, statErr := os.Lstat(journal); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("replayed journal remains: %v", statErr)
				}
			} else {
				preserveJournal()
				if strings.Contains(out.String(), "recovered dependency journal") {
					t.Fatalf("preview replayed the journal:\n%s", out.String())
				}
			}
		})
	}
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairJournalFreePreviewRevalidatesPolicy(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n")
	prepareDevinRepairFixtureOwned(t, repoRoot)
	aggregatePath := filepath.Join(repoRoot, ".devin", harness.DevinMcpConfigFile)
	store, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ws, err := pipeline.DiscoverWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	initial := pipeline.LoadEffective(ctx, pipeline.ConfigLoadOnline, config.LoadEffectiveOptions{
		RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(),
	}, nil)
	if len(initial.Errors) != 0 {
		t.Fatalf("fixture configuration errors: %v", initial.Errors)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	external := append(append([]byte(nil), before...), []byte("# changed after diagnosis\n")...)
	if err := os.WriteFile(configPath, external, 0o644); err != nil {
		t.Fatal(err)
	}
	preserve := snapshotDoctorJournalFiles(t, configPath, aggregatePath, store.StateFile(), store.JournalFile())

	var out bytes.Buffer
	err = runDoctorRepairPlanner(&out, doctorOptions{
		ctx: ctx, resolve: staticResolve(nil),
	}, repoRoot, initial)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("repair error = %v, want policy generation conflict\n%s", err, out.String())
	}
	preserve()
}
