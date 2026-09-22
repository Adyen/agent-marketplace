package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func v2Fixture() ArtifactV2 {
	return ArtifactV2{Manifest: ManifestV2{SchemaVersion: "v2", ArtifactVersion: "v2", Kind: "skill", Name: "review", ContentDigest: "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", Capabilities: []string{}, Provenance: ProvenanceV2{Generator: GeneratorV2}, SupportedSchemas: []string{"v2"}}, Payload: []byte("{}"), Assets: map[string][]byte{"prompt.txt": []byte("review")}, Licenses: map[string][]byte{"LICENSE": []byte("MIT")}}
}

func TestArtifactV2CompressionIndependence(t *testing.T) {
	a := v2Fixture()
	encoded, err := EncodeV2(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Reference.ArtifactDigest != "sha256:ef5b84520825da65c72547c857befbb6c03672d8b9f5ceb778765b96a98dce90" {
		t.Fatalf("independent frame digest mismatch: %s", encoded.Reference.ArtifactDigest)
	}
	decoded, err := VerifyV2(encoded.Bytes, encoded.Reference, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, a.Payload) {
		t.Fatal("payload changed")
	}
	manifest, err := a.Manifest.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	gz, err := gzip.NewWriterLevel(&raw, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	gz.Name = "independent-transport"
	tw := tar.NewWriter(gz)
	for _, entry := range []struct{ name, data string }{{"licenses/LICENSE", "MIT"}, {"assets/prompt.txt", "review"}, {"resource/skill.json", "{}"}, {"manifest.json", string(manifest)}} {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: int64(len(entry.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	ref := encoded.Reference
	ref.BlobDigest = BlobDigest(ContentDigest(raw.Bytes()))
	if ref.BlobDigest == encoded.Reference.BlobDigest {
		t.Fatal("fixture must use different transport bytes")
	}
	if _, err := VerifyV2(raw.Bytes(), ref, nil); err != nil {
		t.Fatal(err)
	}
	raw.Bytes()[0] ^= 1
	if _, err := VerifyV2(raw.Bytes(), ref, nil); !errors.Is(err, ErrBlobDigestMismatch) {
		t.Fatalf("want blob mismatch, got %v", err)
	}
}

func TestArtifactV2CanonicalManifest(t *testing.T) {
	a := v2Fixture()
	const want = `{"schema_version":"v2","artifact_version":"v2","kind":"skill","name":"review","content_digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","capabilities":[],"provenance":{"generator":"agent-marketplace canonical artifact v2"},"supported_schema_versions":["v2"]}`
	got, err := a.Manifest.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("canonical bytes: %s", got)
	}
	if _, err := decodeManifestV2([]byte(want)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{want + "\n", " " + want, `{"schema_version":"v2"}`, string(bytes.Replace([]byte(want), []byte(`"capabilities":[]`), []byte(`"capabilities":null`), 1)), string(bytes.Replace([]byte(want), []byte(`"name":"review"`), []byte(`"name":"review","Name":"review"`), 1)), string(bytes.Replace([]byte(want), []byte(`"name":"review"`), []byte(`"name":"review","name":"review"`), 1))} {
		if _, err := decodeManifestV2([]byte(raw)); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("want manifest error for %q, got %v", raw, err)
		}
	}
}
