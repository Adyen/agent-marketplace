package artifact

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPlaceArchiveBindsExactPackage(t *testing.T) {
	const kind = "herdr-app/mod"
	profile := targetprofile.Identity{
		TargetID:       "herdr-app",
		SourceID:       "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + strings.Repeat("a", 64),
	}
	files := []targetprofile.PackageFile{
		{
			SourcePath:      "src/é/README.md",
			DestinationPath: "modules/README.md",
			Mode:            0o644,
			Bytes:           []byte("# package\n"),
		},
		{
			SourcePath:      "src/nested/资料.bin",
			DestinationPath: "modules/data.bin",
			Mode:            0o755,
			Bytes:           []byte{0x00, 0xff, 0x01},
		},
	}

	artifact, err := NewPlaceArtifact(kind, "example", profile, files)
	if err != nil {
		t.Fatalf("NewPlaceArtifact: %v", err)
	}
	if got, want := PayloadPath(kind), "resource/place-package.json"; got != want {
		t.Fatalf("PayloadPath(%q) = %q, want %q", kind, got, want)
	}
	final, placeholderRaw, digest, err := EncodeArchives(artifact)
	if err != nil {
		t.Fatalf("EncodeArchives: %v", err)
	}
	placeholder, err := VerifyPlaceholderArchive(placeholderRaw, digest)
	if err != nil {
		t.Fatalf("VerifyPlaceholderArchive: %v", err)
	}
	verified, err := VerifyFinalAgainstPlaceholder(final, placeholder)
	if err != nil {
		t.Fatalf("VerifyFinalAgainstPlaceholder: %v", err)
	}
	gotFiles, err := verified.PlaceFiles()
	if err != nil {
		t.Fatalf("PlaceFiles: %v", err)
	}
	wantFiles := []targetprofile.PackageFile{files[1], files[0]}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("PlaceFiles() = %#v, want %#v", gotFiles, wantFiles)
	}

	reversed, err := NewPlaceArtifact(kind, "example", profile, []targetprofile.PackageFile{files[1], files[0]})
	if err != nil {
		t.Fatalf("NewPlaceArtifact in source order: %v", err)
	}
	_, reversedPlaceholder, _, err := EncodeArchives(reversed)
	if err != nil {
		t.Fatalf("EncodeArchives in source order: %v", err)
	}
	if !bytes.Equal(placeholderRaw, reversedPlaceholder) {
		t.Fatal("placeholder archive depends on package file author order")
	}

	changedMapping := bytes.Replace(artifact.Payload, []byte(`"destination_path":"modules/data.bin"`), []byte(`"destination_path":"modules/renamed.bin"`), 1)
	if bytes.Equal(changedMapping, artifact.Payload) {
		t.Fatal("fixture did not change the package destination")
	}
	staleMapping := artifact
	staleMapping.Payload = changedMapping
	requirePlaceArchiveRefused(t, staleMapping, "stale mapping")

	changedMode := bytes.Replace(artifact.Payload, []byte(`"mode":493`), []byte(`"mode":420`), 1)
	if bytes.Equal(changedMode, artifact.Payload) {
		t.Fatal("fixture did not change the executable mode")
	}
	tamperedMode := artifact
	tamperedMode.Payload = changedMode
	requirePlaceArchiveRefused(t, tamperedMode, "tampered mode")

	missingBinding := artifact
	missingBinding.Manifest.HostPackageIdentity = nil
	if _, err := encodeArtifact(missingBinding); err == nil {
		t.Fatal("place manifest without host package identity encoded")
	}

	legacy, err := FromIR(ir.NewSkill("legacy", "Legacy", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	legacy.Manifest.HostPackageIdentity = artifact.Manifest.HostPackageIdentity
	if _, err := encodeArtifact(legacy); err == nil {
		t.Fatal("legacy manifest with host package identity encoded")
	}

	for _, tc := range []struct {
		name    string
		members []string
	}{
		{name: "legacy resource path", members: []string{"resource/skill.json"}},
		{name: "shadow payload", members: []string{"resource/place-package.json", "resource/skill.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := placeArchiveWithPayloadMembers(t, artifact, tc.members...)
			if _, err := VerifyPlaceholderArchive(raw, ArchiveDigest(raw)); err == nil {
				t.Fatal("scoped archive with an invalid payload path verified")
			}
		})
	}
}

func requirePlaceArchiveRefused(t *testing.T, a CanonicalArtifact, name string) {
	t.Helper()
	a.Manifest.ArchiveDigest = archiveDigestPlaceholder
	raw, err := encodeArtifact(a)
	if err != nil {
		t.Fatalf("encodeArtifact for %s: %v", name, err)
	}
	if _, err := VerifyPlaceholderArchive(raw, ArchiveDigest(raw)); err == nil {
		t.Fatalf("%s verified", name)
	}
}

func placeArchiveWithPayloadMembers(t *testing.T, a CanonicalArtifact, members ...string) []byte {
	t.Helper()
	manifest := a.Manifest
	manifest.ArchiveDigest = archiveDigestPlaceholder
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return writeTarGz(t, func(tw *tar.Writer) {
		writeLimitMemberData(t, tw, CanonicalManifestPath, manifestBytes)
		if err := tw.WriteHeader(&tar.Header{Name: CanonicalPayloadDir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
		for _, member := range members {
			writeLimitMemberData(t, tw, member, a.Payload)
		}
	})
}
