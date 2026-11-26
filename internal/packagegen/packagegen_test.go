package packagegen

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPublishGenerationRetriesParentSyncOnReuse(t *testing.T) {
	parent := t.TempDir()
	gen := Generation{Path: filepath.Join(parent, "generation")}
	syncFailure := errors.New("injected parent sync failure")
	var writes, verifies, syncs int
	publish := func() (bool, error) {
		return publishGeneration(gen, parent, []string{"file"}, func(stage string, _ int) error {
			writes++
			return writeGenerationFile(stage, "file", []byte("payload"), 0o644)
		}, func(path string) error {
			verifies++
			data, err := os.ReadFile(filepath.Join(path, "file"))
			if err == nil && string(data) != "payload" {
				t.Fatalf("reused generation contains %q", data)
			}
			return err
		}, func(path string) error {
			if path != parent {
				t.Fatalf("syncing %q, want %q", path, parent)
			}
			syncs++
			if syncs <= 2 {
				return syncFailure
			}
			return syncDirectory(path)
		})
	}
	for i := range 2 {
		if fresh, err := publish(); fresh || !errors.Is(err, syncFailure) {
			t.Fatalf("attempt %d = (%v, %v), want parent sync failure", i+1, fresh, err)
		}
	}
	if fresh, err := publish(); fresh || err != nil {
		t.Fatalf("final retry = (%v, %v), want verified reuse", fresh, err)
	}
	if writes != 1 || verifies != 2 || syncs != 3 {
		t.Fatalf("writes/verifications/parent syncs = %d/%d/%d, want 1/2/3", writes, verifies, syncs)
	}
}

func TestBuildKeyCoversPluginBuildInputsAndSortsExclusions(t *testing.T) {
	base := Input{
		Plugin:             &ir.Plugin{Files: []ir.PluginFile{{Path: "plugin.json", Digest: "a", Executable: false}}},
		PackageRoot:        "/plugins/review",
		Target:             hcc.TargetClaude,
		Delivery:           "decompose",
		Scope:              "repo:abc",
		Exclusions:         []string{"skill/z", "skill/a"},
		CatalogFingerprint: "catalog-a",
	}
	key := BuildKey(base)
	reordered := base
	reordered.Exclusions = []string{"skill/a", "skill/z"}
	if got := BuildKey(reordered); got != key {
		t.Fatalf("reordered exclusions changed build key: %s != %s", got, key)
	}
	for _, mutate := range []func(*Input){
		func(in *Input) { in.Target = hcc.TargetCursor },
		func(in *Input) { in.Delivery = "native" },
		func(in *Input) { in.Scope = "machine" },
		func(in *Input) { in.Exclusions = []string{"skill/a"} },
		func(in *Input) { in.CatalogFingerprint = "catalog-b" },
		func(in *Input) { in.Plugin = &ir.Plugin{Files: []ir.PluginFile{{Path: "plugin.json", Digest: "b"}}} },
	} {
		changed := base
		mutate(&changed)
		if got := BuildKey(changed); got == key {
			t.Fatalf("build key ignored a build input: %+v", changed)
		}
	}
}

func TestPublishRefusesEscapingPackageFileWithoutStagingResidue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", canonicalHome)
	source := filepath.Join(root, "package")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"review"}`)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "outside.txt")
	payload := []byte("outside package")
	if err := os.WriteFile(external, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(source, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	manifestDigest := sha256.Sum256(manifest)
	payloadDigest := sha256.Sum256(payload)
	input := Input{
		Plugin: &ir.Plugin{
			Manifest: ir.Manifest{Name: "review"},
			Files: []ir.PluginFile{
				{Path: "plugin.json", Digest: hex.EncodeToString(manifestDigest[:])},
				{Path: "escape.txt", Digest: hex.EncodeToString(payloadDigest[:])},
			},
		},
		PackageRoot:        source,
		Target:             hcc.TargetClaude,
		Delivery:           "decompose",
		Scope:              "repo:abc",
		CatalogFingerprint: "catalog-a",
	}
	generation, err := Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(input); err == nil || !strings.Contains(err.Error(), "bundle.support-file.unsafe") {
		t.Fatalf("escaping source file error = %v, want bundle.support-file.unsafe", err)
	}
	if _, err := os.Lstat(generation.Path); !os.IsNotExist(err) {
		t.Fatalf("partial package generation remained after refusal: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(generation.Path)); err != nil || len(entries) != 0 {
		t.Fatalf("package staging residue = %v / %v", entries, err)
	}
	got, err := os.ReadFile(external)
	if err != nil || !reflect.DeepEqual(got, payload) {
		t.Fatalf("external source changed: %q / %v", got, err)
	}
}
