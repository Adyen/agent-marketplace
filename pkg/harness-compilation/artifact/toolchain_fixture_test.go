package artifact

import (
	"bytes"
	"errors"
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

func toolchainPlaceholderFixturePath(goMajorMinor string) string {
	return filepath.Join("testdata", "toolchain", goMajorMinor, "mcpserver.placeholder.tar.gz")
}

func readToolchainPlaceholderFixture(t *testing.T, goMajorMinor string) []byte {
	t.Helper()
	raw, err := os.ReadFile(toolchainPlaceholderFixturePath(goMajorMinor))
	if err != nil {
		t.Fatalf("read placeholder fixture: %v", err)
	}
	return raw
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
	final, placeholder, _, err := EncodeArchives(toolchainFixtureArtifact(t))
	if err != nil {
		t.Fatalf("EncodeArchives: %v", err)
	}
	goMajorMinor := strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
	if err := os.MkdirAll(filepath.Dir(toolchainFixturePath(goMajorMinor)), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(toolchainFixturePath(goMajorMinor), final, 0o644); err != nil {
		t.Fatalf("WriteFile final: %v", err)
	}
	if err := os.WriteFile(toolchainPlaceholderFixturePath(goMajorMinor), placeholder, 0o644); err != nil {
		t.Fatalf("WriteFile placeholder: %v", err)
	}
}

// spec: marketplace-api/placeholder-archive-verifies-regardless-of-the-encoding-toolchain
func TestPlaceholderArchiveVerifiesRegardlessOfEncodingToolchain(t *testing.T) {
	for _, recorded := range toolchainFixtureVersions {
		t.Run("recorded-by-"+recorded, func(t *testing.T) {
			final, _ := readToolchainFixture(t, recorded)
			placeholderRaw := readToolchainPlaceholderFixture(t, recorded)
			if _, err := VerifyPlaceholderArchive(placeholderRaw, final.Manifest.ArchiveDigest); err != nil {
				t.Fatalf("VerifyPlaceholderArchive: %v", err)
			}
		})
	}
}

// spec: marketplace-api/final-archive-with-different-bytes-but-identical-content-verifies
func TestFinalArchiveVerifiesAgainstPlaceholderByContent(t *testing.T) {
	for _, recorded := range toolchainFixtureVersions {
		t.Run("recorded-by-"+recorded, func(t *testing.T) {
			final, finalRaw := readToolchainFixture(t, recorded)
			placeholder, err := VerifyPlaceholderArchive(readToolchainPlaceholderFixture(t, recorded), final.Manifest.ArchiveDigest)
			if err != nil {
				t.Fatalf("VerifyPlaceholderArchive: %v", err)
			}
			if _, err := VerifyFinalAgainstPlaceholder(finalRaw, placeholder); err != nil {
				t.Fatalf("VerifyFinalAgainstPlaceholder: %v", err)
			}

			tampered := final
			tampered.Payload = []byte(`{"tampered":true}`)
			tamperedRaw, err := encodeArtifact(tampered)
			if err != nil {
				t.Fatalf("encodeArtifact: %v", err)
			}
			if _, err := VerifyFinalAgainstPlaceholder(tamperedRaw, placeholder); err == nil {
				t.Fatal("tampered final archive verified against placeholder")
			}
		})
	}
}

func TestPlaceholderArchiveRejectsTamperedBytes(t *testing.T) {
	final, _ := readToolchainFixture(t, toolchainFixtureVersions[0])
	raw := readToolchainPlaceholderFixture(t, toolchainFixtureVersions[0])
	raw = append([]byte(nil), raw...)
	raw[len(raw)/2] ^= 0xff
	if _, err := VerifyPlaceholderArchive(raw, final.Manifest.ArchiveDigest); err == nil {
		t.Fatal("tampered placeholder archive verified")
	}
}

func TestDerivePlaceholderArchiveIsCompressorDependent(t *testing.T) {
	current := strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
	for _, recorded := range toolchainFixtureVersions {
		t.Run("recorded-by-"+recorded, func(t *testing.T) {
			final, finalRaw := readToolchainFixture(t, recorded)
			derived, err := DerivePlaceholderArchive(finalRaw, final.Manifest.ArchiveDigest)
			if recorded != current {
				if !errors.Is(err, ErrPlaceholderNotReproducible) {
					t.Fatalf("DerivePlaceholderArchive error = %v, want ErrPlaceholderNotReproducible", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DerivePlaceholderArchive: %v", err)
			}
			if !bytes.Equal(derived, readToolchainPlaceholderFixture(t, recorded)) {
				t.Fatal("derived placeholder archive differs from recorded placeholder fixture")
			}
		})
	}
}

// spec: marketplace-api/received-archive-bytes-verify-across-toolchains
func TestArchiveDigestVerifiesAcrossGoToolchains(t *testing.T) {
	current := strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
	for _, recorded := range toolchainFixtureVersions {
		t.Run("placeholder-recorded-by-"+recorded, func(t *testing.T) {
			final, _ := readToolchainFixture(t, recorded)
			if _, err := VerifyArchiveBytes(readToolchainPlaceholderFixture(t, recorded), final.Manifest.ArchiveDigest); err != nil {
				t.Fatalf("VerifyArchiveBytes(placeholder): %v", err)
			}
		})
		t.Run("final-recorded-by-"+recorded, func(t *testing.T) {
			final, finalRaw := readToolchainFixture(t, recorded)
			_, err := VerifyArchiveBytes(finalRaw, final.Manifest.ArchiveDigest)
			if recorded == current {
				if err != nil {
					t.Fatalf("VerifyArchiveBytes(final, native toolchain): %v", err)
				}
				return
			}
			var mismatch *EncodingMismatchError
			if !errors.As(err, &mismatch) || mismatch.Want != final.Manifest.ArchiveDigest {
				t.Fatalf("VerifyArchiveBytes(final, foreign toolchain) error = %v, want *EncodingMismatchError", err)
			}
		})
	}

	t.Run("tampered-content-is-an-integrity-error", func(t *testing.T) {
		final, _ := readToolchainFixture(t, toolchainFixtureVersions[0])
		final.Payload = []byte(`{"tampered":true}`)
		raw, err := encodeArtifact(final)
		if err != nil {
			t.Fatalf("encodeArtifact: %v", err)
		}
		_, err = VerifyArchiveBytes(raw, final.Manifest.ArchiveDigest)
		var mismatch *EncodingMismatchError
		if err == nil || errors.As(err, &mismatch) {
			t.Fatalf("tampered archive error = %v, want an integrity error that is not an encoding mismatch", err)
		}
	})

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
