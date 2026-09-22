package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

const pythonV2Archive = "H4sIAAAAAAAC/+3WQU+DMBQHcD7K0vN0LXRl7mp2MFEvejNmeeteXR0U0nbTxPjdLUs0ymUXnXG+3wVaCOUB/5YanDUY4uljaFz2M3iipNxtk/6WS1l87O/6hSh5mQ14dgCbEMGn4b+jyM/F/REvLOgV1jDfog+2cWzKtjkbMvDRGtCx37+2bpn2w9pWVWo6qDE1PW4tPqW2blxEF+dL+5A+qu7EFeRjNZVSFMpAMR4vClVOQAipYCmUKXGi5BmeSbOQRufCYJm6CsE1V9wooQGMmUB3aWhhYSsbLQY2vbsfstY3W3TgdLqFF/aADj3ExqdRITXiSQ1+jbGtQONAg2uc1VAN3isbpHpehyxs2rbxEZfzrw+iG6Mr+f41O2YeQ7PxGke7N/ozs8C+/HOe9/Jf8GJM+T9I/o/78yZ7QAgYwyhNpXUbT+Nz/IX1Px3t5T8tGZT/A83/3cpNOfivKqvRBQyjy4vz2fXNLPuV/Pf//3MuOOX/EK4ubikEhBBCCCGEEEIIIYQQQgghR+QNcDpu9QAoAAA="

func TestArtifactV2StaticCompatibility(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(pythonV2Archive)
	if err != nil {
		t.Fatal(err)
	}
	ref := ArtifactReference{SchemaVersion: "v2", ArtifactDigest: "sha256:ef5b84520825da65c72547c857befbb6c03672d8b9f5ceb778765b96a98dce90", BlobDigest: "sha256:6dfc1467ff0b24434f5e60649aa57cef1c1fb5e853296f9e09a80ba51a7f0693", MediaType: MediaTypeV2}
	decoded, err := VerifyV2(raw, ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded.Assets["prompt.txt"]) != "review" || string(decoded.Licenses["LICENSE"]) != "MIT" || string(decoded.Payload) != "{}" {
		t.Fatal("protected entries changed")
	}
}

func v2TestArchive(t *testing.T, mutate func(*tar.Header, []byte) (*tar.Header, []byte), extra []byte) ([]byte, ArtifactReference) {
	t.Helper()
	encoded, err := EncodeV2(v2Fixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(encoded.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		hdr, data = mutate(hdr, data)
		if hdr == nil {
			continue
		}
		hdr.Size = int64(len(data))
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Write(extra); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	ref := encoded.Reference
	ref.BlobDigest = BlobDigest(ContentDigest(buf.Bytes()))
	return buf.Bytes(), ref
}

func TestArtifactV2Tampering(t *testing.T) {
	for _, tt := range []struct {
		name, member, replacement string
		want                      error
	}{
		{"control", "", "", nil},
		{"payload", "resource/skill.json", "{\"changed\":true}", ErrContentDigestMismatch},
		{"asset", "assets/prompt.txt", "changed", ErrArtifactDigestMismatch},
		{"license", "licenses/LICENSE", "changed", ErrArtifactDigestMismatch},
		{"manifest", "manifest.json", "", ErrManifestInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, ref := v2TestArchive(t, func(h *tar.Header, d []byte) (*tar.Header, []byte) {
				if h.Name == tt.member {
					d = []byte(tt.replacement)
				}
				return h, d
			}, nil)
			_, err := VerifyV2(raw, ref, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
		})
	}
}

func TestArtifactV2Structure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*tar.Header, []byte) (*tar.Header, []byte)
		extra  []byte
	}{
		{name: "unexpected", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "assets/prompt.txt" {
				h.Name = "unexpected"
			}
			return h, d
		}},
		{name: "duplicate", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "assets/prompt.txt" {
				h.Name = "manifest.json"
			}
			return h, d
		}},
		{name: "ambiguous", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "assets/prompt.txt" {
				h.Name = "assets/./prompt.txt"
			}
			return h, d
		}},
		{name: "traversal", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "assets/prompt.txt" {
				h.Name = "assets/../prompt.txt"
			}
			return h, d
		}},
		{name: "symlink", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "assets/prompt.txt" {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "elsewhere"
				d = nil
			}
			return h, d
		}},
		{name: "missing-payload", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "resource/skill.json" {
				return nil, nil
			}
			return h, d
		}},
		{name: "wrong-kind-path", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) {
			if h.Name == "resource/skill.json" {
				h.Name = "resource/command.json"
			}
			return h, d
		}},
		{name: "trailing-tar-data", mutate: func(h *tar.Header, d []byte) (*tar.Header, []byte) { return h, d }, extra: []byte("smuggled")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, ref := v2TestArchive(t, tt.mutate, tt.extra)
			if _, err := VerifyV2(raw, ref, nil); !errors.Is(err, ErrArchiveStructure) {
				t.Fatalf("want structure error, got %v", err)
			}
		})
	}
}

func TestArtifactV2ManifestRejectionThroughVerifier(t *testing.T) {
	for name, transform := range map[string]func(string) string{
		"whitespace":    func(s string) string { return s + "\n" },
		"bom":           func(s string) string { return "\xef\xbb\xbf" + s },
		"empty-license": func(s string) string { return strings.Replace(s, `"capabilities":`, `"license":"","capabilities":`, 1) },
		"null":          func(s string) string { return strings.Replace(s, `"capabilities":[]`, `"capabilities":null`, 1) },
		"null-element":  func(s string) string { return strings.Replace(s, `"capabilities":[]`, `"capabilities":[null]`, 1) },
		"unknown":       func(s string) string { return strings.Replace(s, `"generator":`, `"unknown":true,"generator":`, 1) },
		"duplicate": func(s string) string {
			return strings.Replace(s, `"name":"review"`, `"name":"review","name":"review"`, 1)
		},
		"case-alias":      func(s string) string { return strings.Replace(s, `"name":"review"`, `"Name":"review"`, 1) },
		"optional-escape": func(s string) string { return strings.Replace(s, "review", `\u0072eview`, 1) },
		"invalid-utf8": func(s string) string {
			return strings.Replace(s, `"capabilities":[]`, "\"capabilities\":[\"\xff\"]", 1)
		},
		"surrogate": func(s string) string { return strings.Replace(s, `"capabilities":[]`, `"capabilities":["\ud800"]`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			raw, ref := v2TestArchive(t, func(h *tar.Header, d []byte) (*tar.Header, []byte) {
				if h.Name == "manifest.json" {
					d = []byte(transform(string(d)))
				}
				return h, d
			}, nil)
			if _, err := VerifyV2(raw, ref, nil); !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("want manifest error, got %v", err)
			}
		})
	}
}

func TestArtifactV2StringEncoding(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"\"\\\b\t\n\f\r\x00\x01\x1f", `"\"\\\b\t\n\f\r\u0000\u0001\u001f"`},
		{"<>&/é\u2028\u2029", `"<>&/é\u2028\u2029"`},
		{"e\u0301", "\"e\u0301\""},
	} {
		var b bytes.Buffer
		if err := appendCanonicalString(&b, tt.value); err != nil {
			t.Fatal(err)
		}
		if b.String() != tt.want {
			t.Fatalf("got %q, want %q", b.String(), tt.want)
		}
	}
}

func TestArtifactV2ContainerTruncation(t *testing.T) {
	encoded, err := EncodeV2(v2Fixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := unpackArchive(encoded.Bytes, *DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	var truncated bytes.Buffer
	gz := gzip.NewWriter(&truncated)
	if _, err := gz.Write(unpacked[:len(unpacked)-1024]); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{encoded.Bytes[:len(encoded.Bytes)-1], truncated.Bytes(), append(bytes.Clone(encoded.Bytes), encoded.Bytes...)} {
		ref := encoded.Reference
		ref.BlobDigest = BlobDigest(ContentDigest(raw))
		if _, err := VerifyV2(raw, ref, nil); !errors.Is(err, ErrArchiveStructure) {
			t.Fatalf("want container structure error, got %v", err)
		}
	}
}

func TestArtifactV2LimitsAndReferences(t *testing.T) {
	encoded, err := EncodeV2(v2Fixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ArtifactReference)
		want   error
	}{
		{"schema", func(r *ArtifactReference) { r.SchemaVersion = "v99" }, ErrUnsupportedSchema},
		{"malformed", func(r *ArtifactReference) { r.BlobDigest = "bad" }, ErrInvalidReference},
		{"media", func(r *ArtifactReference) { r.MediaType = "application/octet-stream" }, ErrInvalidReference},
		{"artifact", func(r *ArtifactReference) { r.ArtifactDigest = ArtifactDigest(ContentDigest([]byte("wrong"))) }, ErrArtifactDigestMismatch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ref := encoded.Reference
			tt.mutate(&ref)
			if _, err := VerifyV2(encoded.Bytes, ref, nil); !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
		})
	}
	for _, mutate := range []func(*ArchiveLimits){func(l *ArchiveLimits) { l.MaxPayloadBytes = 1 }, func(l *ArchiveLimits) { l.MaxAssetBytes = 1 }, func(l *ArchiveLimits) { l.MaxTotalBytes = 1 }, func(l *ArchiveLimits) { l.MaxTotalFiles = 2 }} {
		limits := DefaultArchiveLimits()
		mutate(limits)
		if _, err := VerifyV2(encoded.Bytes, encoded.Reference, limits); !errors.Is(err, ErrArchiveLimit) {
			t.Fatalf("want limit error, got %v", err)
		}
	}
}
