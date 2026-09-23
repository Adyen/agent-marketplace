package artifact

import (
	"bytes"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func makeBenchmarkArtifact() CanonicalArtifact {
	a, err := FromIR(ir.NewSkill(
		"review",
		"Review code",
		"Review the code carefully.",
		[]string{"user"},
		map[string]any{"author": "bench"},
		ir.SourceRef{},
	))
	if err != nil {
		panic(err)
	}
	a.Assets = map[string][]byte{
		"prompt.md": []byte("# Prompt\n\nReview."),
		"rules.md":  []byte("# Rules\n\nBe kind."),
	}
	a.Licenses = map[string][]byte{
		"LICENSE": []byte("MIT"),
	}

	raw, digest, err := Encode(a)
	if err != nil {
		panic(err)
	}
	a.rawArchive = raw
	a.Manifest.ArchiveDigest = digest
	return a
}

func BenchmarkCanonicalArtifactEncode(b *testing.B) {
	a := makeBenchmarkArtifact()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Encode(a); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactEncodeWithProof(b *testing.B) {
	a := makeBenchmarkArtifact()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := EncodeWithProof(a); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactDecode(b *testing.B) {
	a := makeBenchmarkArtifact()
	raw := a.rawArchive
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Decode(bytes.NewReader(raw)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactDecodeProof(b *testing.B) {
	a := makeBenchmarkArtifact()
	_, proof, digest, err := EncodeWithProof(a)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeProof(proof, digest, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactDecodeCanonical(b *testing.B) {
	a := makeBenchmarkArtifact()
	raw := a.rawArchive
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeCanonical(raw, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactReproduceProof(b *testing.B) {
	a := makeBenchmarkArtifact()
	raw := a.rawArchive
	digest := a.Manifest.ArchiveDigest
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReproduceProof(raw, digest, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactFromIR(b *testing.B) {
	r := ir.NewSkill(
		"review",
		"Review code",
		"Review the code carefully.",
		[]string{"user"},
		map[string]any{"author": "bench"},
		ir.SourceRef{},
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := FromIR(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalArtifactVerifyArchiveDigest(b *testing.B) {
	a := makeBenchmarkArtifact()
	raw := a.rawArchive
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.VerifyArchiveDigest(raw); err != nil {
			b.Fatal(err)
		}
	}
}
