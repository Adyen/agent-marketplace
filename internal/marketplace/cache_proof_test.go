package marketplace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactProofCacheUsesIndependentVerificationEvidence(t *testing.T) {
	cache := NewCache(t.TempDir())
	digest := "sha256:" + strings.Repeat("a", 64)
	path, err := cache.WriteArtifactProof(digest, []byte("proof"))
	if err != nil {
		t.Fatalf("WriteArtifactProof: %v", err)
	}
	if filepath.Base(path) != "artifact.proof.tar.gz" {
		t.Fatalf("proof path = %q", path)
	}
	if _, verified, err := cache.ReadArtifactProof(digest); err != nil || verified {
		t.Fatalf("initial proof verified/error = %v/%v", verified, err)
	}
	if err := cache.MarkArtifactProofVerified(digest); err != nil {
		t.Fatalf("MarkArtifactProofVerified: %v", err)
	}
	if raw, verified, err := cache.ReadArtifactProof(digest); err != nil || !verified || string(raw) != "proof" {
		t.Fatalf("marked proof = %q, %v, %v", raw, verified, err)
	}
	if _, err := cache.WriteArtifact(digest, []byte("final")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if _, verified, err := cache.ReadArtifact(digest); err != nil || verified {
		t.Fatalf("final archive inherited proof evidence: verified/error = %v/%v", verified, err)
	}
	if _, verified, err := cache.ReadArtifactProof(digest); err != nil || !verified {
		t.Fatalf("final write invalidated proof evidence: verified/error = %v/%v", verified, err)
	}
}

func TestArtifactProofOnlyCacheDirectoryIsRemovable(t *testing.T) {
	cache := NewCache(t.TempDir())
	digest := "sha256:" + strings.Repeat("b", 64)
	path, err := cache.WriteArtifactProof(digest, []byte("proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := removeArtifactDirectory(cache.root, filepath.Dir(path)); err != nil {
		t.Fatalf("removeArtifactDirectory: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("proof cache directory still exists: %v", err)
	}
}
