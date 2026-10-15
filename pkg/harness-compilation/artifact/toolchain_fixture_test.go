package artifact

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var toolchainFixtureVersions = []string{"go1.26", "go1.27"}

func toolchainFixturePath(goMajorMinor string) string {
	return filepath.Join("testdata", "toolchain", goMajorMinor, "mcpserver.tar.gz")
}

func toolchainFixtureArtifact(t *testing.T) CanonicalArtifact {
	t.Helper()
	a, err := FromIR(ir.NewMcpServer("genai-docs", "npx", []string{"-y", "@adyen/genai-docs-mcp"}, map[string]string{"GENAI_DOCS_REGION": "eu"}, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a.Manifest.RegistryID = "src"
	a.Manifest.Revision = a.Manifest.ContentDigest
	return a
}

func readToolchainFixture(t *testing.T, goMajorMinor string) (CanonicalArtifact, []byte) {
	t.Helper()
	data, err := os.ReadFile(toolchainFixturePath(goMajorMinor))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	a, raw, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode fixture %s: %v", goMajorMinor, err)
	}
	return a, raw
}

// Run with UPDATE_TOOLCHAIN_FIXTURES=1 under each Go toolchain to record its archive bytes.
func TestGenerateToolchainArchiveFixture(t *testing.T) {
	if os.Getenv("UPDATE_TOOLCHAIN_FIXTURES") != "1" {
		t.Skip("set UPDATE_TOOLCHAIN_FIXTURES=1 to regenerate toolchain archive fixtures")
	}
	raw, _, err := Encode(toolchainFixtureArtifact(t))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	goMajorMinor := strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
	path := toolchainFixturePath(goMajorMinor)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestArchiveDigestVerifiesAcrossGoToolchains(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			a, raw := readToolchainFixture(t, goMajorMinor)
			if err := a.ValidateArchive(nil); err != nil {
				t.Fatalf("ValidateArchive: %v", err)
			}
			if err := a.VerifyArchiveDigest(raw); err != nil {
				t.Fatalf("VerifyArchiveDigest: %v", err)
			}
		})
	}

	t.Run("fixtures-exercise-toolchain-drift", func(t *testing.T) {
		first, _ := readToolchainFixture(t, toolchainFixtureVersions[0])
		second, _ := readToolchainFixture(t, toolchainFixtureVersions[1])
		if first.Manifest.ContentDigest != second.Manifest.ContentDigest {
			t.Fatalf("content digests differ: %s vs %s", first.Manifest.ContentDigest, second.Manifest.ContentDigest)
		}
		if first.Manifest.ArchiveDigest == second.Manifest.ArchiveDigest {
			t.Fatalf("fixtures do not exercise toolchain drift: both archive digests are %s", first.Manifest.ArchiveDigest)
		}
	})
}
