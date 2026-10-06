package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/portable-output-digest-refusal-preserves-native-generation
func TestRunPortableOutputDigestRefusesHardLink(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "digest-probe", Resource: ir.NewSkill("digest-probe", "portable", "original inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("valid portable native control failed: %+v %v", initial, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "digest-probe", "SKILL.md")
	before, err := os.ReadFile(installed)
	if err != nil || !bytes.Contains(before, []byte("original inert body")) {
		t.Fatalf("native control missing content: %q %v", before, err)
	}
	foreign := filepath.Join(t.TempDir(), "retained.md")
	if err := os.Link(installed, foreign); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewSkill("digest-probe", "portable", "changed inert body", nil, nil, ir.SourceRef{})
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("native output digest lost coded refusal: %+v %v", report, err)
	}
	if _, err := portablePathDigest(installed); !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("portable regular digest lost hardlink refusal identity: %v", err)
	}
	for _, path := range []string{installed, foreign} {
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, before) {
			t.Fatalf("digest refusal changed %s bytes: %q %v", path, data, err)
		}
		if after, err := os.Stat(path); err != nil || !os.SameFile(beforeInfo, after) || beforeInfo.Mode() != after.Mode() || !beforeInfo.ModTime().Equal(after.ModTime()) {
			t.Fatalf("digest refusal changed %s generation: %v", path, err)
		}
	}
	if after, err := os.ReadFile(manifest); err != nil || !bytes.Equal(after, manifestBefore) {
		t.Fatalf("digest refusal changed manifest: %q %v", after, err)
	}
	if residue, err := filepath.Glob(filepath.Join(filepath.Dir(installed), ".agent-marketplace-*")); err != nil || len(residue) != 0 {
		t.Fatalf("digest refusal left native staging residue: %v %v", residue, err)
	}
}

func TestPortableRegularDigestRetainsLegacyEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.md")
	if err := os.WriteFile(path, []byte("body"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("file\x00\x000640\x004\x00body"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got, err := portablePathDigest(path); err != nil || got != want {
		t.Fatalf("legacy portable digest changed: %q want %q err %v", got, want, err)
	}
}

func TestPortableRegularDigestRefusesReplacementAfterObservation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output.md")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(root, "retained.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	var digestBytes bytes.Buffer
	if err := writePortableRegularDigest(&digestBytes, path, "", before); !errors.Is(err, fsboundary.ErrGenerationConflict) || digestBytes.Len() != 0 {
		t.Fatalf("replacement regular payload reached ownership digest: %q %v", digestBytes.Bytes(), err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatalf("digest refusal changed replacement: %q %v", data, err)
	}
}
