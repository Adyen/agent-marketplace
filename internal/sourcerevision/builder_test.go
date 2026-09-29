package sourcerevision

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestBuildIsDeterministicCompleteAndIncludesIntrinsicSupport(t *testing.T) {
	snapshot := t.TempDir()
	root := filepath.Join(snapshot, "registry")
	writeFixture(t, filepath.Join(root, ".factory", "skills", "zeta", "SKILL.md"), "---\nname: zeta\ndescription: Zeta\n---\nZeta body.\n")
	writeFixture(t, filepath.Join(root, ".factory", "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha\n---\nAlpha body.\n")
	writeFixture(t, filepath.Join(root, ".factory", "skills", "alpha", "scripts", "run.sh"), "#!/bin/sh\necho alpha\n")
	writeFixture(t, filepath.Join(root, "collections", "starter", "COLLECTION.yaml"), "name: starter\nresources:\n  - kind: skill\n    name: alpha\n")

	input := Input{SnapshotRoot: snapshot, RootPath: "registry"}
	first, err := Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, err := Build(input)
	if err != nil {
		t.Fatalf("Build second time: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Build is not deterministic:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if first.SourceHarness != "factory" {
		t.Fatalf("SourceHarness = %q, want factory", first.SourceHarness)
	}
	if len(first.Manifest.Resources) != 3 {
		t.Fatalf("resources = %+v, want alpha, zeta, and starter", first.Manifest.Resources)
	}
	wantOrder := []string{"collection/starter", "skill/alpha", "skill/zeta"}
	for i, want := range wantOrder {
		got := first.Manifest.Resources[i].Kind + "/" + first.Manifest.Resources[i].Name
		if got != want {
			t.Fatalf("resource[%d] = %q, want %q", i, got, want)
		}
		if first.Manifest.Resources[i].ContentDigest == "" {
			t.Fatalf("resource[%d] has empty content digest", i)
		}
	}
	if first.Manifest.Resources[0].ArchiveDigest != "" {
		t.Fatalf("collection archive digest = %q, want empty", first.Manifest.Resources[0].ArchiveDigest)
	}
	alpha := first.Manifest.Resources[1]
	if alpha.ArchiveDigest == "" {
		t.Fatal("skill archive digest is empty")
	}
	if len(alpha.SupportFiles) != 1 || alpha.SupportFiles["scripts/run.sh"] == "" {
		t.Fatalf("alpha support files = %+v, want scripts/run.sh digest", alpha.SupportFiles)
	}
	if first.Manifest.RevisionDigest == "" || len(first.Payload) == 0 {
		t.Fatalf("manifest digest/payload missing: digest=%q bytes=%d", first.Manifest.RevisionDigest, len(first.Payload))
	}
	if err := verifyManifest(first.Manifest, first.Payload); err != nil {
		t.Fatalf("canonical source manifest verification: %v", err)
	}
}

func TestBuildOmitsUnsupportedRuleWithDeterministicWarning(t *testing.T) {
	snapshot := t.TempDir()
	writeFixture(t, filepath.Join(snapshot, ".factory", "skills", "valid", "SKILL.md"), "---\nname: valid\n---\nvalid body\n")
	writeFixture(t, filepath.Join(snapshot, ".factory", "rules", "end-of-response-ask-user-question.md"), "Ask the user a question.\n")

	first, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatalf("Build second time: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Build is not deterministic:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if len(first.Manifest.Resources) != 1 || first.Manifest.Resources[0].Kind != "skill" || first.Manifest.Resources[0].Name != "valid" {
		t.Fatalf("resources = %+v, want only skill/valid", first.Manifest.Resources)
	}
	if len(first.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want one", first.Warnings)
	}
	warning := first.Warnings[0]
	if warning.Kind != "rule" || warning.Name != "end-of-response-ask-user-question" {
		t.Fatalf("warning = %+v, want rule/end-of-response-ask-user-question", warning)
	}
	verifiedWarnings, err := Verify(Input{SnapshotRoot: snapshot}, first.Projection())
	if err != nil {
		t.Fatalf("Verify filtered projection: %v", err)
	}
	if !reflect.DeepEqual(verifiedWarnings, first.Warnings) {
		t.Fatalf("Verify warnings = %+v, want %+v", verifiedWarnings, first.Warnings)
	}
}

func TestBuildInventoriesValidResourcesAlongsideMalformedDiagnostics(t *testing.T) {
	snapshot := t.TempDir()
	writeFixture(t, filepath.Join(snapshot, ".factory", "skills", "valid", "SKILL.md"), "---\nname: valid\n---\nvalid body\n")
	writeFixture(t, filepath.Join(snapshot, ".factory", "skills", "broken", "SKILL.md"), "---\nname: [not-a-string\n---\nbody\n")

	first, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatalf("Build with malformed sibling: %v", err)
	}
	second, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatalf("Build second time: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Build is not deterministic:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if len(first.Manifest.Resources) != 1 {
		t.Fatalf("resources = %+v, want only successfully ingested valid skill", first.Manifest.Resources)
	}
	resource := first.Manifest.Resources[0]
	if resource.Kind != "skill" || resource.Name != "valid" || resource.ContentDigest == "" || resource.ArchiveDigest == "" {
		t.Fatalf("resource = %+v, want complete valid skill projection", resource)
	}
	if err := verifyManifest(first.Manifest, first.Payload); err != nil {
		t.Fatalf("canonical source manifest verification: %v", err)
	}
}

func TestBuildRejectsTraversalAndSymlinkEscape(t *testing.T) {
	snapshot := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, filepath.Join(snapshot, "safe", ".factory", "skills", "ok", "SKILL.md"), "---\nname: ok\n---\nbody\n")

	t.Run("lexical root traversal", func(t *testing.T) {
		_, err := Build(Input{SnapshotRoot: snapshot, RootPath: "../" + filepath.Base(outside), SourceHarness: "factory"})
		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Build error = %v, want ErrPathEscape", err)
		}
	})

	t.Run("root symlink escape", func(t *testing.T) {
		if err := os.Symlink(outside, filepath.Join(snapshot, "escaped")); err != nil {
			t.Fatal(err)
		}
		_, err := Build(Input{SnapshotRoot: snapshot, RootPath: "escaped", SourceHarness: "factory"})
		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Build error = %v, want ErrPathEscape", err)
		}
	})

	t.Run("nested symlink escape", func(t *testing.T) {
		if err := os.Symlink(outside, filepath.Join(snapshot, "safe", "outside-link")); err != nil {
			t.Fatal(err)
		}
		_, err := Build(Input{SnapshotRoot: snapshot, RootPath: "safe", SourceHarness: "factory"})
		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Build error = %v, want ErrPathEscape", err)
		}
	})

	// spec: registry-symlink-containment/narrowed-source-root-excludes-unsafe-symlink
	t.Run("unsafe symlink outside configured root is ignored", func(t *testing.T) {
		root := filepath.Join(snapshot, "scoped")
		writeFixture(t, filepath.Join(root, ".factory", "skills", "ok", "SKILL.md"), "---\nname: ok\n---\nbody\n")
		if err := os.MkdirAll(filepath.Join(snapshot, ".factory"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/nonexistent/settings.json", filepath.Join(snapshot, ".factory", "settings.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(Input{SnapshotRoot: snapshot, RootPath: "scoped", SourceHarness: "factory"}); err != nil {
			t.Fatalf("Build(scoped root) error = %v", err)
		}
		_, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Build(repository root) error = %v, want ErrPathEscape", err)
		}
		for _, want := range []string{`".factory/settings.json"`, `"/nonexistent/settings.json"`, "remove it from the registry"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Build error %q does not mention %s", err, want)
			}
		}
	})

	// spec: registry-symlink-containment/selected-unsafe-symlink-identifies-remediation
	t.Run("in-root unsafe symlink error identifies remediation", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			link   string
			target string
		}{
			{name: "dangling", link: "dangling-link", target: "/nonexistent/.factory/settings.json"},
			{name: "escaping", link: "escape-link", target: outside},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fresh := t.TempDir()
				root := filepath.Join(fresh, "diag")
				writeFixture(t, filepath.Join(root, ".factory", "skills", "ok", "SKILL.md"), "---\nname: ok\n---\nbody\n")
				if err := os.Symlink(tc.target, filepath.Join(root, tc.link)); err != nil {
					t.Fatal(err)
				}
				_, err := Build(Input{SnapshotRoot: fresh, RootPath: "diag", SourceHarness: "factory"})
				if !errors.Is(err, ErrPathEscape) {
					t.Fatalf("Build error = %v, want ErrPathEscape", err)
				}
				if tc.name == "dangling" && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Build error = %v, want os.ErrNotExist cause", err)
				}
				for _, want := range []string{`"diag/` + tc.link + `"`, strconv.Quote(tc.target), `"diag"`, "narrow the registry entry's path"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("Build error %q does not mention %s", err, want)
					}
				}
			})
		}
	})

	t.Run("nested symlink cannot widen configured root", func(t *testing.T) {
		root := filepath.Join(snapshot, "narrow")
		writeFixture(t, filepath.Join(root, ".factory", "skills", "ok", "SKILL.md"), "---\nname: ok\n---\nbody\n")
		if err := os.Symlink(filepath.Join(snapshot, "safe"), filepath.Join(root, "sibling-link")); err != nil {
			t.Fatal(err)
		}
		_, err := Build(Input{SnapshotRoot: snapshot, RootPath: "narrow", SourceHarness: "factory"})
		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Build error = %v, want ErrPathEscape", err)
		}
	})
}

func TestVerifyDetectsManifestAndFilesystemTampering(t *testing.T) {
	snapshot := t.TempDir()
	skill := filepath.Join(snapshot, ".factory", "skills", "alpha", "SKILL.md")
	writeFixture(t, skill, "---\nname: alpha\n---\noriginal\n")
	input := Input{SnapshotRoot: snapshot}
	built, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	locked := built.Projection()
	if _, err := Verify(input, locked); err != nil {
		t.Fatalf("Verify unchanged snapshot: %v", err)
	}

	t.Run("locked field", func(t *testing.T) {
		tampered := locked
		tampered.Resources = append([]revisionResource(nil), locked.Resources...)
		tampered.Resources[0].ContentDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		if _, err := Verify(input, tampered); !errors.Is(err, ErrManifestMismatch) {
			t.Fatalf("Verify error = %v, want ErrManifestMismatch", err)
		}
	})

	writeFixture(t, skill, "---\nname: alpha\n---\ntampered\n")
	if _, err := Verify(input, locked); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("Verify filesystem tamper error = %v, want ErrManifestMismatch", err)
	}
}

func TestBuildRejectsSupportFileSymlink(t *testing.T) {
	snapshot := t.TempDir()
	skillDir := filepath.Join(snapshot, ".factory", "skills", "alpha")
	writeFixture(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: alpha\n---\nbody\n")
	outside := filepath.Join(t.TempDir(), "secret")
	writeFixture(t, outside, "secret")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}

	_, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("Build error = %v, want ErrPathEscape", err)
	}
}

func writeFixture(tb testing.TB, name, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
}
