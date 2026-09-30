package pipeline

// Phase observer boundary tests (AC5). This file intentionally imports only
// the standard library plus internal non-TUI packages so it can be copied to
// the base commit: there it must fail to build because the AC5 observer
// symbols (WithPhaseObserver, Phase, PhaseConfigure, PhaseResolve,
// PhaseMaterialize, PhasePrepare) do not exist yet — a missing-symbol red,
// not a missing-module failure.

import (
	"context"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func phaseObserverTestLoaders() Loaders {
	return Loaders{
		Org:    func() (*config.Config, error) { return &config.Config{}, nil },
		Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
		Repo:   func() (*config.Config, error) { return &config.Config{}, nil },
		Local:  func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
	}
}

func TestSyncReportsPhaseBoundariesInOrder(t *testing.T) {
	var observed []Phase
	ctx := WithPhaseObserver(context.Background(), func(phase Phase) {
		observed = append(observed, phase)
	})
	// An empty configuration still walks the real pipeline boundaries:
	// configuration load, catalog/resolution, then materialization.
	result := Sync(ctx, ResolveOptions{Workspace: mustWorkspace(t, t.TempDir()), Loaders: phaseObserverTestLoaders()})
	if len(result.Errors) > 0 {
		t.Logf("sync returned errors (phases still must precede them): %v", result.Errors)
	}
	for _, want := range []Phase{PhaseConfigure, PhaseResolve, PhaseMaterialize} {
		if !slices.Contains(observed, want) {
			t.Fatalf("phase %s was not reported; observed %v", want, observed)
		}
	}
	configure := slices.Index(observed, PhaseConfigure)
	resolve := slices.Index(observed, PhaseResolve)
	materialize := slices.Index(observed, PhaseMaterialize)
	if configure >= resolve || resolve >= materialize {
		t.Fatalf("phases reported out of order: %v", observed)
	}
}

func TestPrepareMarketplaceInstallReportsPreparePhase(t *testing.T) {
	var observed []Phase
	ctx := WithPhaseObserver(context.Background(), func(phase Phase) {
		observed = append(observed, phase)
	})
	// Invalid options still cross the prepare boundary before failing.
	if _, err := PrepareMarketplaceInstall(ctx, InstallOptions{}); err == nil {
		t.Fatal("expected invalid marketplace install options to fail")
	}
	if !slices.Contains(observed, PhasePrepare) {
		t.Fatalf("prepare phase was not reported; observed %v", observed)
	}
}

func TestPhaseObserverIsOptional(t *testing.T) {
	// Callers that attach no observer must run the same pipeline without
	// panicking or changing results.
	result := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, t.TempDir()), Loaders: phaseObserverTestLoaders()})
	if len(result.Errors) > 0 {
		t.Fatalf("unobserved sync changed behavior: %v", result.Errors)
	}
}
