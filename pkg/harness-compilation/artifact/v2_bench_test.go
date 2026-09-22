package artifact

import (
	"bytes"
	"testing"
)

func BenchmarkArtifactVerification(b *testing.B) {
	source := makeBenchmarkArtifact()
	source.Payload = []byte(`{"name":"review","body":"` + string(bytes.Repeat([]byte("Review source carefully. "), 2048)) + `"}`)
	source.Manifest.ContentDigest = ContentDigest(source.Payload)
	source.Assets["reference.txt"] = bytes.Repeat([]byte("Reference instructions and examples.\n"), 8192)
	raw, digest, err := Encode(source)
	if err != nil {
		b.Fatal(err)
	}
	ref := LegacyReference{ArchiveDigest: digest, Kind: source.Manifest.Kind, Name: source.Manifest.Name, ContentDigest: source.Manifest.ContentDigest}
	converted, _, err := ConvertV1ToV2(raw, ref, nil)
	if err != nil {
		b.Fatal(err)
	}
	v2, err := EncodeV2(converted, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("V1", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		b.ResetTimer()
		for b.Loop() {
			a, _, err := Decode(bytes.NewReader(raw))
			if err != nil {
				b.Fatal(err)
			}
			if err := a.ValidateArchive(nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("V2", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(v2.Bytes)))
		b.ResetTimer()
		for b.Loop() {
			if _, err := VerifyV2(v2.Bytes, v2.Reference, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}
