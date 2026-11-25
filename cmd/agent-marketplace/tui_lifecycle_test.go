package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func assertNoArchiveRefreshWarnings(t *testing.T, warnings []error) {
	t.Helper()
	for _, w := range warnings {
		var refresh *resolver.ArchiveEvidenceRefreshedWarning
		if errors.As(w, &refresh) {
			t.Fatalf("reported a lock refresh that never persisted: %v", w)
		}
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestTUIDependencyLifecycleRefreshesDriftedLockArchiveEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]

	for _, upgrade := range []bool{false, true} {
		operation := "sync"
		if upgrade {
			operation = "update"
		}
		t.Run(operation, func(t *testing.T) {
			newDigest := drift()
			lifecycle, err := session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
			warnings := lifecycle.Warnings
			if err != nil {
				t.Fatalf("TUI %s after archive drift: %v", operation, err)
			}
			var refreshed *resolver.ArchiveEvidenceRefreshedWarning
			for _, w := range warnings {
				if errors.As(w, &refreshed) {
					break
				}
			}
			if refreshed == nil || refreshed.NewArchiveDigest != newDigest {
				t.Fatalf("TUI %s warnings = %v; want archive-evidence refresh to %q", operation, warnings, newDigest)
			}
			record := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
			if string(record.ArchiveDigest) != newDigest {
				t.Fatalf("TUI %s left lock archive digest %q, want refreshed %q", operation, record.ArchiveDigest, newDigest)
			}
			if record.ContentDigest != oldRecord.ContentDigest || record.Revision != oldRecord.Revision {
				t.Fatalf("TUI %s changed revision identity: %#v", operation, record)
			}
			body, err := os.ReadFile(filepath.Join(home, ".factory", "skills", "review", "SKILL.md"))
			if err != nil || !strings.Contains(string(body), "Review carefully") {
				t.Fatalf("TUI %s materialized output broken: %v", operation, err)
			}

			lockBefore, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err = session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
			warnings = lifecycle.Warnings
			if err != nil {
				t.Fatalf("healthy TUI %s: %v", operation, err)
			}
			for _, w := range warnings {
				var repeat *resolver.ArchiveEvidenceRefreshedWarning
				if errors.As(w, &repeat) {
					t.Fatalf("healthy TUI %s emitted a refresh warning: %v", operation, w)
				}
			}
			lockAfter, err := os.ReadFile(lockPath)
			if err != nil || string(lockAfter) != string(lockBefore) {
				t.Fatalf("healthy TUI %s rewrote the lock: %v", operation, err)
			}
		})
	}
}

func TestTUIDependencyLifecycleReusesSourceRevisionAcrossPreparationAndResolve(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo := setupLocalGitSyncFixture(t, 1)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed exact v4 lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	for action := 1; action <= 2; action++ {
		var logs bytes.Buffer
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
		result, err := session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false)
		slog.SetDefault(previousLogger)
		if err != nil {
			t.Fatalf("TUI sync lifecycle %d: %v", action, err)
		}
		if builds := strings.Count(logs.String(), `msg="built direct-Git source revision"`); builds != 1 {
			t.Fatalf("TUI sync lifecycle %d built the same locked source %d times across preparation and resolve, want one\n%s", action, builds, logs.String())
		}
		if len(result.Errors) != 0 {
			t.Fatalf("TUI sync lifecycle %d returned errors: %v", action, result.Errors)
		}
		installed, err := os.ReadFile(filepath.Join(repo, ".factory", "skills", "review-0", "SKILL.md"))
		if err != nil || !strings.Contains(string(installed), "# Review") {
			t.Fatalf("TUI sync lifecycle %d broke materialized skill: %v\n%s", action, err, installed)
		}
		lockAfter, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatalf("TUI sync lifecycle %d changed exact lock bytes: %v\nbefore: %s\nafter:  %s", action, err, lockBefore, lockAfter)
		}
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestTUIDependencyLifecycleFailedReacquisitionPreservesLastGood(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, failArtifact := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
	installedPath := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	installedBefore, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatal(err)
	}
	failArtifact(drift())

	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false)
	warnings := lifecycle.Warnings
	if err == nil {
		t.Fatal("TUI sync with unreachable advertised artifact succeeded")
	}
	assertNoArchiveRefreshWarnings(t, warnings)
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("failed reacquisition rewrote the lock: %v", err)
	}
	installedAfter, err := os.ReadFile(installedPath)
	if err != nil || string(installedAfter) != string(installedBefore) {
		t.Fatalf("failed reacquisition touched installed output: %v", err)
	}
	record := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
	if record.ArchiveDigest != oldRecord.ArchiveDigest || record.ContentDigest != oldRecord.ContentDigest {
		t.Fatalf("failed reacquisition changed evidence: %#v", record)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestTUIDependencyLifecycleRefreshedLockPublicationFailurePreservesLastGood(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
	installedPath := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	installedBefore, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatal(err)
	}
	drift()

	commit := lifecycle.CommitDependencyTransaction
	var mutated bool
	lifecycle.CommitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		for _, file := range desired {
			if file.Path == lockPath && !file.Delete {
				if err := os.Chmod(lockPath, file.ExpectedMode^0o020); err != nil {
					t.Fatal(err)
				}
				mutated = true
			}
		}
		return commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { lifecycle.CommitDependencyTransaction = commit })

	for _, upgrade := range []bool{false, true} {
		operation := "sync"
		if upgrade {
			operation = "update"
		}
		t.Run(operation, func(t *testing.T) {
			lockBefore, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			mutated = false
			lifecycle, err := session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
			warnings := lifecycle.Warnings
			if err == nil {
				t.Fatalf("TUI %s committed refreshed evidence onto a lock whose mode drifted", operation)
			}
			if !strings.Contains(err.Error(), "refresh Marketplace lock evidence") {
				t.Fatalf("TUI %s failure does not identify the evidence refresh: %v", operation, err)
			}
			assertNoArchiveRefreshWarnings(t, warnings)
			if !mutated {
				t.Fatalf("TUI %s never attempted the refreshed Marketplace lock commit", operation)
			}
			lockAfter, err := os.ReadFile(lockPath)
			if err != nil || string(lockAfter) != string(lockBefore) {
				t.Fatalf("failed TUI %s rewrote the lock: %v", operation, err)
			}
			installedAfter, err := os.ReadFile(installedPath)
			if err != nil || string(installedAfter) != string(installedBefore) {
				t.Fatalf("failed TUI %s touched installed output: %v", operation, err)
			}
			record := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
			if record.ArchiveDigest != oldRecord.ArchiveDigest || record.ContentDigest != oldRecord.ContentDigest {
				t.Fatalf("failed TUI %s changed lock evidence: %#v", operation, record)
			}
		})
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestTUIDependencyLifecycleSkippedRefreshDropsClaimButKeepsOtherWarnings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
	drift()
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	unrelatedWarning := errors.New("unrelated catalog warning")
	unrelatedFailure := errors.New("unrelated materialization failure")
	real := session.RunTUIExactSync
	session.RunTUIExactSync = func(ctx context.Context, options pipeline.ResolveOptions) pipeline.SyncResult {
		result := real(ctx, options)
		result.Warnings = append(result.Warnings, unrelatedWarning)
		result.Errors = append(result.Errors, unrelatedFailure)
		return result
	}
	t.Cleanup(func() { session.RunTUIExactSync = real })

	lifecycle, err := session.RuntimeDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false)

	warnings := lifecycle.Warnings
	if !errors.Is(err, unrelatedFailure) {
		t.Fatalf("TUI sync error = %v; want the unrelated failure", err)
	}
	if !slices.Contains(warnings, unrelatedWarning) {
		t.Fatalf("TUI sync dropped the unrelated warning: %v", warnings)
	}
	assertNoArchiveRefreshWarnings(t, warnings)
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("skipped TUI refresh rewrote the lock: %v", err)
	}
	record := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]
	if record.ArchiveDigest != oldRecord.ArchiveDigest || record.ContentDigest != oldRecord.ContentDigest {
		t.Fatalf("skipped TUI refresh changed lock evidence: %#v", record)
	}
}
