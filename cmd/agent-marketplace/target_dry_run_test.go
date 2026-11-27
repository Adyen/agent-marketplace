package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

type targetDryRunFixture struct {
	root, profile, source, manifest, home, destination string
	before                                             map[string]targetDryRunFileState
}

type targetDryRunFileState struct {
	mode    os.FileMode
	modTime time.Time
	bytes   []byte
	link    string
}

const targetDryRunManifestYAML = `apiVersion: agent-marketplace.io/target-import/v1
target_id: invented-app
kind: invented-app/mod
name: sample-links
mappings:
  - source_path: mods/sample-links/main.js
    destination_path: mods/sample-links/main.js
  - source_path: mods/sample-links/mod.json
    destination_path: mods/sample-links/mod.json
display_files: [README.md]
metadata:
  requires: ["invented-app/mod:sample-tabs"]
  manual_setup: [Bind the test key in Invented App settings.]
`

// spec: target-profile-import/valid-local-inputs-produce-a-json-import-report
// spec: target-profile-import/payload-mappings-preserve-bytes-and-list-source-exclusions
// spec: target-profile-import/source-siblings-are-never-imported
// spec: target-profile-import/unmet-prerequisites-and-manual-setup-remain-advisory
// spec: target-profile-import/local-dry-run-needs-no-configuration-approval-acquisition-or-host-execution
func TestTargetDryRunReportsLocalImport(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	manifest := strings.Replace(
		targetDryRunManifestYAML,
		`requires: ["invented-app/mod:sample-tabs"]`,
		`requires:
    - "invented-app/mod:sample-tabs"
    - {kind: other-app/mod, name: sample-tabs, registry: sample-registry, path: profiles/shared}`,
		1,
	)
	fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), manifest)
	stdout, stderr, err := executeTargetDryRun(fixture, false)
	if err != nil {
		t.Fatalf("target dry-run failed: %v\nstderr:\n%s", err, stderr)
	}
	var report targetDryRunReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode target dry-run report: %v\nstdout:\n%s", err, stdout)
	}
	if report.SchemaVersion != 1 || report.Status != "valid" ||
		report.Profile.ID != "invented-app" || report.Profile.Revision == "" ||
		report.Package.TargetID != "invented-app" || report.Package.Kind != "invented-app/mod" ||
		report.Package.Name != "sample-links" {
		t.Fatalf("unexpected target dry-run identity or status: %#v", report)
	}
	if len(report.Mappings) != 2 ||
		report.Mappings[0].SourcePath != "mods/sample-links/main.js" ||
		report.Mappings[0].DestinationPath != "mods/sample-links/main.js" ||
		report.Mappings[1].SourcePath != "mods/sample-links/mod.json" ||
		report.Mappings[1].DestinationPath != "mods/sample-links/mod.json" {
		t.Fatalf("mappings = %#v", report.Mappings)
	}
	excluded := map[string]string{}
	for _, file := range report.ExcludedFiles {
		excluded[file.SourcePath] = file.Reason
	}
	for path, reason := range map[string]string{
		"README.md":                "display",
		"notes/idea.md":            "not-selected",
		"mods/sample-tabs/main.js": "not-selected",
	} {
		if excluded[path] != reason {
			t.Errorf("excluded file %q reason = %q, want %q", path, excluded[path], reason)
		}
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", report.Diagnostics)
	}
	if report.DestinationCheck.Status != "not-requested" ||
		len(report.DestinationCheck.Conflicts) != 0 ||
		len(report.DestinationCheck.OwnershipChanges) != 0 {
		t.Fatalf("destination check = %#v", report.DestinationCheck)
	}
	if len(report.Requirements) != 2 ||
		report.Requirements[0].Ref != "invented-app/mod:sample-tabs" ||
		report.Requirements[0].Status != "unmet" ||
		report.Requirements[1].Ref != `other-app/mod:sample-tabs [registry="sample-registry", path="profiles/shared"]` ||
		report.Requirements[1].Status != "unmet" {
		t.Fatalf("requirements = %#v", report.Requirements)
	}
	if len(report.ManualSetup) != 1 ||
		report.ManualSetup[0].Status != "pending" ||
		report.ManualSetup[0].Text != "Bind the test key in Invented App settings." {
		t.Fatalf("manual setup = %#v", report.ManualSetup)
	}
	assertTargetDryRunFixtureUnchanged(t, fixture)
}

// spec: target-profile-import/refusal-returns-diagnostics-and-a-nonzero-exit
// spec: target-profile-import/malformed-import-manifests-refuse-before-accepting-a-package
// spec: target-profile-import/invalid-or-mismatched-package-identity-refuses
// spec: target-profile-import/unsafe-or-duplicate-mappings-refuse
// spec: target-profile-import/shipped-names-and-missing-packaged-dependencies-refuse
func TestTargetDryRunRefusesInvalidImports(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	valid := targetDryRunManifestYAML
	tests := []struct {
		name       string
		profile    []byte
		manifest   string
		sourceEdit func(*testing.T, targetDryRunFixture)
		wantPath   string
	}{
		{"duplicate key", nil, strings.Replace(valid, "name: sample-links\n", "name: sample-links\nname: other\n", 1), nil, "import.yaml"},
		{"alias", nil, strings.Replace(strings.Replace(valid, "display_files: [README.md]", "display_files: &display [README.md]", 1),
			"requires: [\"invented-app/mod:sample-tabs\"]", "requires: [*display]", 1), nil, "import.yaml"},
		{"merge key", nil, strings.Replace(valid, "metadata:\n", "metadata:\n  <<: {manual_setup: [merged]}\n", 1), nil, "import.yaml"},
		{"unknown field", nil, strings.Replace(valid, "name: sample-links\n", "name: sample-links\nunexpected: true\n", 1), nil, "unexpected"},
		{"invalid API version", nil, strings.Replace(valid, "agent-marketplace.io/target-import/v1", "agent-marketplace.io/target-import/v2", 1), nil, "apiVersion"},
		{"malformed mapping value", nil, strings.Replace(valid, "destination_path: mods/sample-links/main.js", "destination_path: 8", 1), nil, "destination_path"},
		{"second YAML document", nil, valid + "---\nname: another\n", nil, "import.yaml"},
		{"invalid target ID", nil, strings.Replace(valid, "target_id: invented-app", "target_id: Invented-App", 1), nil, "target_id"},
		{"mismatched target ID", nil, strings.Replace(valid, "target_id: invented-app", "target_id: other-app", 1), nil, "target_id"},
		{"mismatched scoped kind", nil, strings.Replace(valid, "kind: invented-app/mod", "kind: other-app/mod", 1), nil, "kind"},
		{"invalid name", nil, strings.Replace(valid, "name: sample-links", "name: ../sample-links", 1), nil, "name"},
		{"traversal source", nil, strings.Replace(valid, "source_path: mods/sample-links/main.js", "source_path: ../outside.js", 1), nil, "../outside.js"},
		{"backslash path", nil, strings.Replace(valid, "source_path: mods/sample-links/main.js", `source_path: mods\sample-links\main.js`, 1), nil, "main.js"},
		{"duplicate source", nil, strings.Replace(valid, "source_path: mods/sample-links/mod.json", "source_path: mods/sample-links/main.js", 1), nil, "main.js"},
		{"duplicate destination", nil, strings.Replace(valid, "destination_path: mods/sample-links/mod.json", "destination_path: mods/sample-links/main.js", 1), nil, "main.js"},
		{"missing mapped helper", nil, strings.Replace(valid, "display_files:", "  - source_path: mods/sample-links/helper.js\n    destination_path: mods/sample-links/helper.js\ndisplay_files:", 1), nil, "helper.js"},
		{"shipped package name", []byte(strings.Replace(targetApproveProfileYAML, "kinds:", "host: {shipped: {mod: [sample-links]}}\nkinds:", 1)), valid, nil, "sample-links"},
		{"selected symlink", nil, valid, func(t *testing.T, fixture targetDryRunFixture) {
			path := filepath.Join(fixture.source, "mods", "sample-links", "main.js")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(fixture.source, "README.md"), path); err != nil {
				t.Fatal(err)
			}
		}, "main.js"},
		{"selected FIFO", nil, valid, func(t *testing.T, fixture targetDryRunFixture) {
			path := filepath.Join(fixture.source, "mods", "sample-links", "main.js")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "main.js"},
		{"invalid profile", []byte("apiVersion: wrong\n"), valid, nil, "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := tt.profile
			if profile == nil {
				profile = []byte(targetApproveProfileYAML)
			}
			fixture := newTargetDryRunFixture(t, profile, tt.manifest)
			if tt.sourceEdit != nil {
				tt.sourceEdit(t, fixture)
				refreshTargetDryRunSnapshot(t, &fixture)
			}
			report := assertTargetDryRunRefused(t, fixture, false, tt.wantPath)
			if len(report.Mappings) != 0 {
				t.Errorf("refused report accepted mappings: %#v", report.Mappings)
			}
			assertTargetDryRunFixtureUnchanged(t, fixture)
		})
	}
}

// spec: target-profile-import/refusal-returns-diagnostics-and-a-nonzero-exit
// spec: target-profile-import/unsafe-or-duplicate-mappings-refuse
// spec: target-profile-import/shipped-names-and-missing-packaged-dependencies-refuse
func TestTargetDryRunDiagnosticsNamePackageAndDestination(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("missing mapped file names package and source path", func(t *testing.T) {
		manifest := strings.Replace(
			targetDryRunManifestYAML,
			"source_path: mods/sample-links/main.js",
			"source_path: mods/sample-links/missing.js",
			1,
		)
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), manifest)
		report := assertTargetDryRunRefused(t, fixture, false, "mods/sample-links/missing.js")
		if len(report.Diagnostics) != 1 {
			t.Fatalf("refused report = %#v", report)
		}
		diagnostic := report.Diagnostics[0]
		if diagnostic.Code != "target.import.mapped-file-invalid" ||
			diagnostic.Path != "mods/sample-links/missing.js" ||
			!strings.Contains(diagnostic.Message, "invented-app/mod/sample-links") {
			t.Fatalf("missing-file diagnostic = %#v", diagnostic)
		}
	})
	t.Run("outside allowlist names refused destination", func(t *testing.T) {
		manifest := strings.Replace(
			targetDryRunManifestYAML,
			"destination_path: mods/sample-links/main.js",
			"destination_path: other/sample-links/main.js",
			1,
		)
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), manifest)
		report := assertTargetDryRunRefused(t, fixture, false, "other/sample-links/main.js")
		if len(report.Diagnostics) != 1 {
			t.Fatalf("refused report = %#v", report)
		}
		diagnostic := report.Diagnostics[0]
		if diagnostic.Code != "target.import.destination-invalid" ||
			diagnostic.Path != "other/sample-links/main.js" {
			t.Fatalf("outside-allowlist diagnostic = %#v", diagnostic)
		}
	})
	t.Run("explicit empty destination refuses", func(t *testing.T) {
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), targetDryRunManifestYAML)
		fixture.destination = ""
		report := assertTargetDryRunRefused(t, fixture, true, "destination")
		if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "target.import.destination-invalid" {
			t.Fatalf("empty destination diagnostic = %#v", report.Diagnostics)
		}
	})
}

// spec: target-profile-import/destination-collision-refuses-without-adopting-identical-bytes
// spec: target-profile-import/new-destination-paths-report-predicted-claims
// spec: target-profile-import/unchanged-owned-paths-are-not-new-claims
// spec: target-profile-import/refused-whole-package-preview-has-no-ownership-changes
// spec: target-profile-import/dry-run-leaves-all-existing-state-unchanged
func TestTargetDryRunDestinationPreview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("new paths predict claims", func(t *testing.T) {
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), targetDryRunManifestYAML)
		stdout, stderr, err := executeTargetDryRun(fixture, true)
		if err != nil {
			t.Fatalf("target dry-run failed: %v\nstderr:\n%s", err, stderr)
		}
		var report targetDryRunReport
		if err := json.Unmarshal(stdout, &report); err != nil {
			t.Fatalf("decode target dry-run report: %v\nstdout:\n%s", err, stdout)
		}
		var wire map[string]any
		if err := json.Unmarshal(stdout, &wire); err != nil {
			t.Fatalf("decode target dry-run wire report: %v\nstdout:\n%s", err, stdout)
		}
		destinationCheck, ok := wire["destination_check"].(map[string]any)
		if !ok {
			t.Fatalf("destination_check wire value = %#v", wire["destination_check"])
		}
		wireChanges, ok := destinationCheck["ownership_changes"].([]any)
		if !ok || len(wireChanges) != 2 {
			t.Fatalf("ownership_changes wire value = %#v", destinationCheck["ownership_changes"])
		}
		for i, path := range []string{"mods/sample-links/main.js", "mods/sample-links/mod.json"} {
			change, ok := wireChanges[i].(map[string]any)
			if !ok || len(change) != 2 ||
				change["destination_path"] != path || change["action"] != "claim" {
				t.Errorf("ownership change wire value[%d] = %#v", i, wireChanges[i])
			}
		}
		if report.Status != "valid" || report.DestinationCheck.Status != "checked" {
			t.Fatalf("destination report = %#v", report.DestinationCheck)
		}
		assertTargetDryRunFixtureUnchanged(t, fixture)
	})
	t.Run("unrelated installed payload is not read and its paths stay reserved", func(t *testing.T) {
		owned := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, owned)
		binding, err := pipeline.PrepareDeployedPlaceApproval(
			context.Background(), mustTargetWorkspace(t, owned), owned.identity.TargetID,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetprofile.RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		_, unrelated := resolvedTargetApprovalPackageWithFiles(t, owned, []targetprofile.PackageFile{{
			SourcePath: "payload/unrelated.js", DestinationPath: "mods/unrelated/guide.js",
			Mode: 0o644, Bytes: []byte("unrelated package\n"),
		}, {
			SourcePath: "payload/colliding.js", DestinationPath: "mods/sample-links/main.js",
			Mode: 0o644, Bytes: []byte("owned collision\n"),
		}})
		if _, err := pipeline.CommitPlaceUpserts(
			context.Background(), binding, owned.profileBytes, targetApprovalEnvironment(t, owned),
			[]marketplace.ResolvedPlacePackage{unrelated},
		); err != nil {
			t.Fatalf("seed unrelated place package: %v", err)
		}
		unrelatedPath := filepath.Join(owned.targetRoot, "mods", "unrelated", "guide.js")
		if err := os.Remove(unrelatedPath); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(unrelatedPath, 0o600); err != nil {
			t.Fatal(err)
		}

		fixture := newTargetDryRunFixture(t, owned.profileBytes, targetDryRunManifestYAML)
		t.Setenv("HOME", owned.home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(owned.home, ".config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(owned.home, ".cache"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(owned.home, ".local", "state"))
		t.Setenv("INVENTED_APP_HOME", owned.targetRoot)
		fixture.destination = owned.targetRoot
		stdout, _, err := executeTargetDryRun(fixture, true)
		if err == nil {
			t.Fatalf("target dry-run accepted an owned destination collision:\n%s", stdout)
		}
		var report targetDryRunReport
		if err := json.Unmarshal(stdout, &report); err != nil {
			t.Fatalf("decode target dry-run report: %v\nstdout:\n%s", err, stdout)
		}
		if report.Status != "refused" || report.DestinationCheck.Status != "checked" ||
			len(report.DestinationCheck.Conflicts) != 1 ||
			report.DestinationCheck.Conflicts[0].DestinationPath != "mods/sample-links/main.js" ||
			len(report.DestinationCheck.OwnershipChanges) != 0 {
			t.Fatalf("destination report = %#v", report)
		}
	})
	t.Run("identical unowned file refuses whole preview", func(t *testing.T) {
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), targetDryRunManifestYAML)
		path := filepath.Join(fixture.destination, "mods", "sample-links", "main.js")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		want := []byte("export const value = 1;\n")
		writeTargetDryRunFile(t, path, want, 0o644)
		refreshTargetDryRunSnapshot(t, &fixture)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		report := assertTargetDryRunRefused(t, fixture, true, "mods/sample-links/main.js")
		if report.DestinationCheck.Status != "checked" ||
			len(report.DestinationCheck.Conflicts) == 0 ||
			len(report.DestinationCheck.OwnershipChanges) != 0 {
			t.Fatalf("refused destination report = %#v", report.DestinationCheck)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) || after.Mode() != before.Mode() ||
			!after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("unowned collision changed: bytes=%q mode=%v mtime=%v err=%v", got, after.Mode(), after.ModTime(), err)
		}
	})
	t.Run("destination outside admitted root refuses", func(t *testing.T) {
		fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), targetDryRunManifestYAML)
		fixture.destination = filepath.Join(fixture.home, "outside")
		if err := os.MkdirAll(fixture.destination, 0o755); err != nil {
			t.Fatal(err)
		}
		refreshTargetDryRunSnapshot(t, &fixture)
		report := assertTargetDryRunRefused(t, fixture, true, "destination")
		if report.DestinationCheck.Status != "refused" ||
			len(report.DestinationCheck.OwnershipChanges) != 0 {
			t.Fatalf("refused destination report = %#v", report.DestinationCheck)
		}
		assertTargetDryRunFixtureUnchanged(t, fixture)
	})
	t.Run("unchanged owned path is omitted", func(t *testing.T) {
		owned := newTargetApprovalFixture(t)
		warmTargetApprovalProfileCache(t, owned)
		binding, err := pipeline.PrepareDeployedPlaceApproval(
			context.Background(), mustTargetWorkspace(t, owned), owned.identity.TargetID,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetprofile.RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		files, pkg := resolvedTargetApprovalPackage(t, owned)
		if _, err := pipeline.CommitPlaceUpserts(
			context.Background(), binding, owned.profileBytes, targetApprovalEnvironment(t, owned),
			[]marketplace.ResolvedPlacePackage{pkg},
		); err != nil {
			t.Fatalf("seed owned place package: %v", err)
		}

		fixture := newTargetDryRunFixture(t, owned.profileBytes, `apiVersion: agent-marketplace.io/target-import/v1
target_id: invented-app
kind: invented-app/mod
name: sample
mappings:
  - source_path: mods/sample/guide.md
    destination_path: mods/sample/guide.md
`)
		t.Setenv("HOME", owned.home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(owned.home, ".config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(owned.home, ".cache"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(owned.home, ".local", "state"))
		t.Setenv("INVENTED_APP_HOME", owned.targetRoot)
		t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "false")
		fixture.destination = owned.targetRoot
		guidePath := filepath.Join(fixture.source, "mods", "sample", "guide.md")
		if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTargetDryRunFile(t, guidePath, files[0].Bytes, 0o644)
		refreshTargetDryRunSnapshot(t, &fixture)
		targetBefore := snapshotTargetDryRunTree(t, owned.targetRoot)
		t.Setenv(ownership.LockTimeoutEnvVar, "0")
		holder, err := ownership.Open(owned.targetRoot, ownership.WithStateRoot(owned.stateRoot))
		if err != nil {
			t.Fatal(err)
		}
		release, err := holder.Lock()
		if err != nil {
			t.Fatalf("hold destination ownership lock: %v", err)
		}
		stateBefore := snapshotTargetDryRunTree(t, owned.stateRoot)
		stdout, stderr, err := executeTargetDryRun(fixture, true)
		release()
		if err != nil {
			t.Fatalf("target dry-run failed: %v\nstderr:\n%s", err, stderr)
		}
		var report targetDryRunReport
		if err := json.Unmarshal(stdout, &report); err != nil {
			t.Fatalf("decode target dry-run report: %v\nstdout:\n%s", err, stdout)
		}
		if report.Status != "valid" || report.DestinationCheck.Status != "checked" ||
			len(report.DestinationCheck.OwnershipChanges) != 0 {
			t.Fatalf("unchanged owned path was reported as a transition: %#v", report.DestinationCheck)
		}
		assertTargetDryRunFixtureUnchanged(t, fixture)
		if targetAfter := snapshotTargetDryRunTree(t, owned.targetRoot); !reflect.DeepEqual(targetAfter, targetBefore) {
			t.Fatalf("dry-run changed owned target:\nbefore: %#v\nafter: %#v", targetBefore, targetAfter)
		}
		if stateAfter := snapshotTargetDryRunTree(t, owned.stateRoot); !reflect.DeepEqual(stateAfter, stateBefore) {
			t.Fatalf("dry-run changed ownership or approval state:\nbefore: %#v\nafter: %#v", stateBefore, stateAfter)
		}
	})
}

func newTargetDryRunFixture(t *testing.T, profile []byte, manifest string) targetDryRunFixture {
	t.Helper()
	root := t.TempDir()
	inputs := filepath.Join(root, "inputs")
	source := filepath.Join(inputs, "source")
	for _, dir := range []string{
		inputs,
		source,
		filepath.Join(source, "mods", "sample-links"),
		filepath.Join(source, "mods", "sample-tabs"),
		filepath.Join(source, "notes"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	profilePath := filepath.Join(inputs, "profile.yaml")
	manifestPath := filepath.Join(inputs, "import.yaml")
	writeTargetDryRunFile(t, profilePath, profile, 0o600)
	writeTargetDryRunFile(t, manifestPath, []byte(manifest), 0o600)
	writeTargetDryRunFile(t, filepath.Join(source, "mods", "sample-links", "main.js"), []byte("export const value = 1;\n"), 0o644)
	writeTargetDryRunFile(t, filepath.Join(source, "mods", "sample-links", "mod.json"), []byte("{\"name\":\"sample-links\"}\n"), 0o644)
	writeTargetDryRunFile(t, filepath.Join(source, "mods", "sample-tabs", "main.js"), []byte("export const sibling = true;\n"), 0o644)
	writeTargetDryRunFile(t, filepath.Join(source, "README.md"), []byte("display\n"), 0o644)
	writeTargetDryRunFile(t, filepath.Join(source, "notes", "idea.md"), []byte("not selected\n"), 0o644)

	home := filepath.Join(root, "home")
	destination := filepath.Join(home, ".config", "invented-app")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("INVENTED_APP_HOME", destination)
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "false")
	fixture := targetDryRunFixture{
		root: root, profile: profilePath, source: source, manifest: manifestPath,
		home: home, destination: destination,
	}
	refreshTargetDryRunSnapshot(t, &fixture)
	return fixture
}

func writeTargetDryRunFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func executeTargetDryRun(fixture targetDryRunFixture, withDestination bool) ([]byte, []byte, error) {
	args := []string{
		"target", "dry-run",
		"--profile", fixture.profile,
		"--source", fixture.source,
		"--import", fixture.manifest,
	}
	if withDestination {
		args = append(args, "--destination", fixture.destination)
	}
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.Bytes(), stderr.Bytes(), err
}

func assertTargetDryRunFixtureUnchanged(t *testing.T, fixture targetDryRunFixture) {
	t.Helper()
	after := snapshotTargetDryRunTree(t, fixture.root)
	if !reflect.DeepEqual(after, fixture.before) {
		t.Errorf("dry-run changed local files or state:\nbefore: %#v\nafter: %#v", fixture.before, after)
	}
}

func refreshTargetDryRunSnapshot(t *testing.T, fixture *targetDryRunFixture) {
	t.Helper()
	fixture.before = snapshotTargetDryRunTree(t, fixture.root)
}

func snapshotTargetDryRunTree(t *testing.T, root string) map[string]targetDryRunFileState {
	t.Helper()
	states := make(map[string]targetDryRunFileState)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state := targetDryRunFileState{mode: info.Mode(), modTime: info.ModTime()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			state.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			state.bytes, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		states[relative] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func assertTargetDryRunRefused(
	t *testing.T,
	fixture targetDryRunFixture,
	withDestination bool,
	wantPath string,
) targetDryRunReport {
	t.Helper()
	stdout, stderr, err := executeTargetDryRun(fixture, withDestination)
	if err == nil {
		t.Fatalf("target dry-run succeeded; stdout:\n%s", stdout)
	}
	var report targetDryRunReport
	if decodeErr := json.Unmarshal(stdout, &report); decodeErr != nil {
		t.Fatalf("decode refused report: %v\nstdout:\n%s\nstderr:\n%s", decodeErr, stdout, stderr)
	}
	if report.SchemaVersion != 1 || report.Status != "refused" || len(report.Diagnostics) == 0 {
		t.Fatalf("refused report = %#v", report)
	}
	foundPath := wantPath == ""
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "" {
			t.Errorf("diagnostic has no stable code: %#v", diagnostic)
		}
		if wantPath == "" || strings.Contains(diagnostic.Path+diagnostic.Message, wantPath) {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("diagnostics %#v do not name %q", report.Diagnostics, wantPath)
	}
	if len(report.DestinationCheck.OwnershipChanges) != 0 {
		t.Errorf("refused report predicted claims: %#v", report.DestinationCheck.OwnershipChanges)
	}
	return report
}
