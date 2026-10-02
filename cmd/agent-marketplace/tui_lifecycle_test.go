package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
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
			warnings, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
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
			warnings, err = runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
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

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
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
	if _, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false); err == nil {
		t.Fatal("TUI sync with unreachable advertised artifact succeeded")
	}
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

	commit := commitDependencyTransaction
	var mutated bool
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
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
	t.Cleanup(func() { commitDependencyTransaction = commit })

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
			_, err = runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), upgrade)
			if err == nil {
				t.Fatalf("TUI %s committed refreshed evidence onto a lock whose mode drifted", operation)
			}
			if !strings.Contains(err.Error(), "refresh Marketplace lock evidence") {
				t.Fatalf("TUI %s failure does not identify the evidence refresh: %v", operation, err)
			}
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
