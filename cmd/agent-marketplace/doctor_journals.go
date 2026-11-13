package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	repairplanner "github.com/Adyen/agent-marketplace/internal/repair"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func doctorJournalSteps(repoRoot string, out io.Writer, refreshPolicy func(context.Context) error) ([]repairplanner.Step, error) {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return nil, fmt.Errorf("inspect dependency transaction state: %w", err)
	}
	pendingErr := dependencytxn.CheckPending(stateDir)
	var steps []repairplanner.Step
	if errors.Is(pendingErr, dependencytxn.ErrRecoveryRequired) {
		entries, err := os.ReadDir(stateDir)
		if err != nil {
			return nil, fmt.Errorf("inspect dependency transaction journals: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".dependencytxn-") || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(stateDir, entry.Name())
			steps = append(steps, repairplanner.Step{
				Key: "dependency-journal-" + entry.Name(), Stage: repairplanner.StageJournal,
				Class: repairplanner.Safe, Action: "replay interrupted dependency journal",
				Code: "dependency.recovery-required", Files: []string{path},
				Inputs: []repairplanner.Input{"journal"},
				Apply: func(ctx context.Context) (bool, error) {
					if err := pipeline.RecoverDependencyTransactions(ctx); err != nil {
						return true, err
					}
					if _, err := fmt.Fprintf(out, "recovered dependency journal: %s\n", path); err != nil {
						return true, err
					}
					return true, refreshPolicy(ctx)
				},
			})
		}
	} else if pendingErr != nil {
		return nil, fmt.Errorf("inspect dependency transaction journals: %w", pendingErr)
	}

	seen := make(map[string]struct{})
	for _, definition := range target.Definitions() {
		for _, machineScope := range []bool{false, true} {
			destinations, err := harness.ResolveDestinations(hcc.Target(definition.Target), machineScope, repoRoot)
			if err != nil {
				return nil, fmt.Errorf("resolve %s ownership destinations: %w", definition.Target, err)
			}
			for _, destination := range destinations {
				store, err := ownership.Open(destination.Root)
				if err != nil {
					return nil, fmt.Errorf("open %s ownership store: %w", destination.ID, err)
				}
				journalPath := store.JournalFile()
				canonicalDir, err := ownership.CanonicalRoot(filepath.Dir(journalPath))
				if err != nil {
					return nil, fmt.Errorf("canonicalize ownership journal path: %w", err)
				}
				journalPath = filepath.Join(canonicalDir, filepath.Base(journalPath))
				if _, duplicate := seen[journalPath]; duplicate {
					continue
				}
				seen[journalPath] = struct{}{}
				if _, err := os.Lstat(journalPath); err != nil {
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					return nil, fmt.Errorf("inspect ownership journal %s: %w", journalPath, err)
				}
				steps = append(steps, repairplanner.Step{
					Key: "ownership-journal-" + journalPath, Stage: repairplanner.StageJournal,
					Class: repairplanner.Safe, Action: "recover ownership journal",
					Code: diag.CodeOwnershipRecovered, Files: []string{journalPath},
					Inputs: []repairplanner.Input{"journal"},
					Apply: func(context.Context) (bool, error) {
						outcomes, err := store.Recover()
						if err != nil {
							return true, err
						}
						for _, outcome := range outcomes {
							if _, err := fmt.Fprintf(out,
								"recovered ownership journal: file=%s generation=%d action=%s\n",
								outcome.File, outcome.Generation, outcome.Action); err != nil {
								return true, err
							}
						}
						return true, nil
					},
				})
			}
		}
	}
	return steps, nil
}
