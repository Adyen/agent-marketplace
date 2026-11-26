package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

// spec: target-profile-place/a-failed-place-root-defers-checks-for-admitted-work
// spec: target-profile-place/completed-check-outcomes-preserve-pass-and-fail-evidence
// spec: target-profile-place/a-skipped-unusable-target-does-not-block-the-admitted-work-barrier
func TestPlaceInitialLoadWaitsForAllAdmittedRoots(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const destination = "mods/sample/guide.md"
	b := newTargetApprovalFixtureWithProfile(t, []byte(targetVerifyProfileYAML))
	a := b
	a.profileBytes = []byte(strings.NewReplacer("invented-app", "first-app", "INVENTED_APP_HOME", "FIRST_APP_HOME",
		"target-verify-fixture", "first-host").Replace(targetVerifyProfileYAML))
	a.identity.TargetID = "first-app"
	a.identity.RevisionDigest = "sha256:" + dependencytxn.Digest(a.profileBytes)
	a.identity.SourceID = strings.ReplaceAll(b.identity.SourceID, "invented-app.yaml", "first-app.yaml")
	a.targetRoot = filepath.Join(b.home, ".config", "first-app")
	if err := os.Mkdir(a.targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIRST_APP_HOME", a.targetRoot)
	first := serveHostedPlaceSelection(t, a, "first-market", destination)
	second := serveHostedPlaceSelection(t, b, "market-id", destination)
	config := fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [first-app, invented-app]
sources:
  first: {marketplace: %q}
  second: {marketplace: %q}
resources:
  first:
    include:
      - tools/target-profile/first-app: {revision: %s}
      - tools/collection/bundle: {revision: %s}
  second:
    include:
      - tools/target-profile/invented-app: {revision: %s}
      - tools/collection/bundle: {revision: %s}
`, first.endpoint, second.endpoint, a.identity.RevisionDigest, first.manifestDigest, b.identity.RevisionDigest, second.manifestDigest)
	write := func(path, data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(b.userConfig, config, 0o600)
	if err := os.Remove(b.userLock); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(b.home, "initial-load.log")
	allowFirst := filepath.Join(b.home, "allow-first")
	stale := filepath.Join(b.targetRoot, "mods/stale.md")
	// Every host launch is counted. Even a deliberately failing check must
	// observe the later root's *new* file and the removal of its stale file.
	host := "#!/bin/sh\nif [ -e " + targetVerifyShellQuote(stale) + " ] || ! grep -qx 'scoped leaf' " +
		targetVerifyShellQuote(filepath.Join(b.targetRoot, destination)) + "; then echo early >> " + targetVerifyShellQuote(log) + "; fi\n"
	write(filepath.Join(bin, "first-host"), host+"echo A >> "+targetVerifyShellQuote(log)+"\ntest -e "+targetVerifyShellQuote(allowFirst)+"\n", 0o700)
	write(filepath.Join(bin, "target-verify-fixture"), host+"echo B >> "+targetVerifyShellQuote(log)+"\n", 0o700)
	if out, err := executeCommand("sync"); err != nil || !strings.Contains(out, "target.approval.required") {
		t.Fatalf("bootstrap sync: %v\n%s", err, out)
	}
	workspace := mustTargetWorkspace(t, b)
	approve := func(id string) targetprofile.ApprovalBinding {
		t.Helper()
		binding, err := pipeline.PrepareDeployedPlaceApproval(context.Background(), workspace, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetprofile.RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		return binding
	}
	approve(a.identity.TargetID)
	binding := approve(b.identity.TargetID)
	// Seed an older owned B generation. Updating it must replace guide.md
	// and prune stale.md, but not while the user's edit is present.
	files, old := resolvedTargetApprovalPackage(t, b)
	old.Entry.URL = second.endpoint
	// A separately owned package from a now deselected source must be pruned.
	_, deselected := resolvedTargetApprovalPackageWithFiles(t, b, []targetprofile.PackageFile{
		{SourcePath: "payload/stale.md", DestinationPath: "mods/stale.md", Bytes: []byte("stale\n"), Mode: 0o644},
	})
	if _, err := pipeline.CommitPlaceUpserts(context.Background(), binding, b.profileBytes, targetApprovalEnvironment(t, b),
		[]marketplace.ResolvedPlacePackage{old, deselected}); err != nil {
		t.Fatal(err)
	}
	guide := filepath.Join(b.targetRoot, destination)
	write(guide, "my edit\n", 0o644)
	assertStatus := func(id, state string) {
		t.Helper()
		out, err := executeCommand("status")
		if err == nil {
			for line := range strings.SplitSeq(statusSection(out, "PLACE PACKAGES"), "\n") {
				if strings.Contains(line, id+"/mod/sample") && strings.Contains(line, state) && strings.Contains(line, "in-sync") {
					return
				}
			}
		}
		t.Fatalf("status: want %s %s in-sync: %v\n%s", id, state, err, out)
	}
	assertRuns := func(want ...string) {
		t.Helper()
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) && len(want) == 0 {
			return
		}
		got := strings.Fields(string(data))
		slices.Sort(got)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("host launches = %q (%v), want %q", data, err, want)
		}
	}
	for range 2 {
		out, err := executeCommand("sync")
		if err == nil || !strings.Contains(err.Error(), "commit place upserts for target invented-app") ||
			!strings.Contains(err.Error(), "place upsert refused at "+destination) {
			t.Fatalf("sync should refuse B's edit: %v\n%s", err, out)
		}
		if err := assertRecoveredTargetFile(guide, []byte("my edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := assertRecoveredTargetFile(stale, []byte("stale\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertStatus(a.identity.TargetID, "verification-pending")
		assertRuns()
	}
	write(guide, string(files[0].Bytes), files[0].Mode)
	if out, err := executeCommand("sync"); err != nil || !strings.Contains(out, "target.place.check-failed") {
		t.Fatalf("unblocked sync should warn about A's failed check: %v\n%s", err, out)
	}
	assertStatus(a.identity.TargetID, "failed")
	assertStatus(b.identity.TargetID, "passed")
	if out, err := executeCommand("sync", "--check", "--offline"); err != nil {
		t.Fatalf("read-only check after placement: %v\n%s", err, out)
	}
	assertRuns("A", "B")

	// B is a collection member that cannot be admitted on this invocation.
	// A's unchanged failed check is still eligible, exactly once.
	write(allowFirst, "", 0o600)
	t.Setenv("INVENTED_APP_HOME", filepath.Join(b.home, "missing-root"))
	if out, err := executeCommand("sync"); err != nil || !strings.Contains(out, "target.place.collection-member-skipped") {
		t.Fatalf("sync should skip the unusable collection target: %v\n%s", err, out)
	}
	assertStatus(a.identity.TargetID, "passed")
	assertRuns("A", "A", "B")
	t.Setenv("INVENTED_APP_HOME", b.targetRoot)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync after both checks passed: %v\n%s", err, out)
	}
	assertStatus(b.identity.TargetID, "passed")
	assertRuns("A", "A", "B")
}
