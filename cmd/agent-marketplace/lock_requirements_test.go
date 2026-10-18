package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

func TestAcquireGitRequirementsPreservesPolicies(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	if seeded.Roots[0].RevisionDigest != "sha256:93bb84495b5553bcc91bb8caccc7001da2f20190b7036de484e6efe43bc3fc91" {
		t.Fatalf("changed frozen fixture revision: %+v", seeded.Roots[0])
	}
	for _, tc := range []struct {
		name     string
		policies []string
		roots    []string
	}{
		{"implicit-explicit", []string{"", "claude"}, []string{"claude"}},
		{"explicit-implicit", []string{"claude", ""}, []string{"claude"}},
		{"distinct-harnesses", []string{"claude", "factory"}, []string{"claude", "factory"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			var entries []config.RegistryEntry
			for _, hint := range tc.policies {
				entries = append(entries, config.RegistryEntry{URL: seeded.URL, Ref: seeded.Ref, SourceHarness: hint})
			}
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
			if err != nil || got.URL != seeded.URL || got.Ref != seeded.Ref || got.Commit != seeded.Commit || len(got.Roots) != len(tc.roots) {
				t.Fatalf("policy acquisition: %v, registry=%+v", err, got)
			}
			for i, harness := range tc.roots {
				want := seeded.Roots[0]
				want.SourceHarness = harness
				if !reflect.DeepEqual(got.Roots[i], want) {
					t.Fatalf("changed source inventory: %+v; want %+v", got.Roots[i], want)
				}
			}
			if builds := strings.Count(logs.String(), `msg="built direct-Git source revision"`); builds != 2 {
				t.Fatalf("requested policies must be independently built: got %d\n%s", builds, logs.String())
			}
		})
	}
}

func TestAcquireGitRequirementsBuildsDuplicatesOnce(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, hint := range []string{"", "claude"} {
		entries := make([]config.RegistryEntry, 32)
		for i := range entries {
			path := ""
			if i%2 == 0 {
				path = "."
			}
			entries[i] = config.RegistryEntry{URL: seeded.URL, Ref: seeded.Ref, Path: path, SourceHarness: hint}
		}
		for range 2 {
			logs.Reset()
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
			if err != nil || !reflect.DeepEqual(got, seeded) {
				t.Fatalf("duplicate acquisition changed exact inventory: %v, registry=%+v", err, got)
			}
			if builds := strings.Count(logs.String(), `msg="built direct-Git source revision"`); builds != 1 {
				t.Fatalf("duplicate requirements built %d times, want one per invocation", builds)
			}
		}
	}
}

func TestAcquireGitRequirementsRetainsRefusals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	for _, tc := range []struct {
		name  string
		entry config.RegistryEntry
		want  error
	}{
		{"path-escape", config.RegistryEntry{Path: "../outside", SourceHarness: "claude"}, sourcerevision.ErrPathEscape},
		{"invalid-policy", config.RegistryEntry{SourceHarness: "not-a-harness"}, sourcerevision.ErrCatalog},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []config.RegistryEntry{{SourceHarness: "claude"}, tc.entry}
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
			if !errors.Is(err, tc.want) || len(got.Roots) != 0 || got.Commit.String() != "" {
				t.Fatalf("changed fail-closed refusal: registry=%+v, %v; want %v", got, err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLockedRegistry(ctx, seeded.URL, seeded.URL, seeded.Ref, []config.RegistryEntry{{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("changed cancellation: %v", err)
	}
}

func TestAcquireGitRequirementsPreservesDifferentRoots(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	entries := []config.RegistryEntry{{Path: ".", SourceHarness: "claude"}, {Path: "skills", SourceHarness: "claude"}, {Path: "skills", SourceHarness: "claude"}}
	got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
	if err != nil || got.Commit != seeded.Commit || len(got.Roots) != 2 || !reflect.DeepEqual(got.Roots[0], seeded.Roots[0]) {
		t.Fatalf("different-root acquisition: %v, registry=%+v", err, got)
	}
	second := got.Roots[1]
	if second.Path != "skills" || second.SourceHarness != "claude" || second.RevisionSchema != "v4" || len(second.Resources) != 1 || second.Resources[0].Name != "review-0" {
		t.Fatalf("distinct root was conflated or lost: %+v", second)
	}
}

func TestSyncRepeatedGitRequirementsKeepsExactLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, seeded := setupGitRequirementsFixture(t)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"
	for _, alias := range []string{"first", "second", "third"} {
		body += "  " + alias + ": {git: " + seeded.URL + ", ref: main}\n"
	}
	body += "resources:\n  first: [skill/review-0]\n  second: [skill/review-0]\n  third: [skill/review-0]\n"
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), body)
	logPath := filepath.Join(t.TempDir(), "update.jsonl")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logPath)
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
	for _, args := range [][]string{{"update"}, {"sync", "--locked"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		if args[0] == "update" {
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			acquisition, verification, reconciled := strings.Cut(string(logs), `"msg":"lock layer reconciled"`)
			buildRecord := `"msg":"built direct-Git source revision"`
			if builds := strings.Count(acquisition, buildRecord); !reconciled || builds != 1 {
				t.Fatalf("update acquisition: reconciled=%t, builds=%d; want one before reconciliation", reconciled, builds)
			}
			verification, _, verified := strings.Cut(verification, `"msg":"lock snapshot verification completed"`)
			if builds := strings.Count(verification, buildRecord); !verified || builds != 1 {
				t.Fatalf("update verification: completed=%t, builds=%d; want one independent verification", verified, builds)
			}
		}
		after, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("repeated requirements changed committed lock: %v\n%s", err, after)
		}
	}
	assertMaterializedSkillContains(t, repo, "review-0", "Review")
}
