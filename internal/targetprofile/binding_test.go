package targetprofile

import (
	"io/fs"
	"strings"
	"testing"
)

func TestTargetPlaceBinding(t *testing.T) {
	profile := Identity{
		TargetID:       "herdr-app",
		SourceID:       "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + strings.Repeat("a", 64),
	}
	files := []PackageFile{
		{
			SourcePath:      "mods/é/helper.js",
			DestinationPath: "mods/example/bin/helper.js",
			Mode:            fs.FileMode(0o755),
			Bytes:           []byte("#!/usr/bin/env node\n"),
		},
		{
			SourcePath:      "mods/e\u0301/main.js",
			DestinationPath: "mods/example/main.js",
			Mode:            fs.FileMode(0o644),
			Bytes:           []byte("one\n"),
		},
	}
	originalFirst := files[0].SourcePath
	binding, err := BuildBinding(profile, files)
	if err != nil {
		t.Fatalf("BuildBinding() error = %v", err)
	}
	if binding.PackageContentDigest != "sha256:716ca2754d95bd7c209bea10cb6e0b4b2299c913c27b5ddfed7b3214e2acfeac" {
		t.Errorf("PackageContentDigest = %q", binding.PackageContentDigest)
	}
	if binding.DeliveryMappingDigest != "sha256:d6ec45c2deeae6ccd7b5a4082c48100c4e2fa0761a21784d2b25a6fff370b09a" {
		t.Errorf("DeliveryMappingDigest = %q", binding.DeliveryMappingDigest)
	}
	if binding.ProfileDigest != profile.RevisionDigest || binding.ProfileIdentity != profile {
		t.Errorf("profile binding = %#v, want profile %#v", binding, profile)
	}
	reversed, err := BuildBinding(profile, []PackageFile{files[1], files[0]})
	if err != nil {
		t.Fatalf("BuildBinding(reversed) error = %v", err)
	}
	if reversed != binding {
		t.Errorf("BuildBinding() changed with file order: %#v vs %#v", binding, reversed)
	}
	if files[0].SourcePath != originalFirst {
		t.Errorf("BuildBinding() mutated input order: first source = %q", files[0].SourcePath)
	}
	if err := binding.ValidateFor("herdr-app/mod", binding.PackageContentDigest); err != nil {
		t.Errorf("ValidateFor() error = %v", err)
	}
	if err := binding.MatchProfile(profile); err != nil {
		t.Errorf("MatchProfile() error = %v", err)
	}

	for _, tt := range []struct {
		name string
		edit func([]PackageFile) []PackageFile
	}{
		{"empty file map", func([]PackageFile) []PackageFile { return nil }},
		{"duplicate source", func(files []PackageFile) []PackageFile {
			files[1].SourcePath = files[0].SourcePath
			return files
		}},
		{"duplicate destination", func(files []PackageFile) []PackageFile {
			files[1].DestinationPath = files[0].DestinationPath
			return files
		}},
		{"traversal source", func(files []PackageFile) []PackageFile {
			files[0].SourcePath = "../outside"
			return files
		}},
		{"invalid UTF-8 destination", func(files []PackageFile) []PackageFile {
			files[0].DestinationPath = string([]byte{0xff})
			return files
		}},
		{"unsupported mode", func(files []PackageFile) []PackageFile {
			files[0].Mode = 0o600
			return files
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := append([]PackageFile(nil), files...)
			if _, err := BuildBinding(profile, tt.edit(input)); err == nil {
				t.Fatal("BuildBinding() error = nil, want refusal")
			}
		})
	}

	otherSource := profile
	otherSource.SourceID = "directory:%2Ftmp%2Fprofiles"
	if err := binding.MatchProfile(otherSource); err == nil {
		t.Error("MatchProfile() accepted a different source with the same revision")
	}
	otherRevision := profile
	otherRevision.RevisionDigest = "sha256:" + strings.Repeat("b", 64)
	if err := binding.MatchProfile(otherRevision); err == nil {
		t.Error("MatchProfile() accepted the same source with a different revision")
	}
	for _, tt := range []struct {
		name          string
		kind          string
		contentDigest string
		edit          func(*Binding)
	}{
		{"different scoped target", "other/mod", binding.PackageContentDigest, func(*Binding) {}},
		{"ordinary kind", "mod", binding.PackageContentDigest, func(*Binding) {}},
		{"content mismatch", "herdr-app/mod", "sha256:" + strings.Repeat("b", 64), func(*Binding) {}},
		{"invalid mapping digest", "herdr-app/mod", binding.PackageContentDigest, func(b *Binding) { b.DeliveryMappingDigest = "bad" }},
		{"profile digest mismatch", "herdr-app/mod", binding.PackageContentDigest, func(b *Binding) { b.ProfileDigest = "sha256:" + strings.Repeat("b", 64) }},
		{"invalid profile identity", "herdr-app/mod", binding.PackageContentDigest, func(b *Binding) { b.ProfileIdentity.SourceID = "bad" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := binding
			tt.edit(&candidate)
			if err := candidate.ValidateFor(tt.kind, tt.contentDigest); err == nil {
				t.Fatal("ValidateFor() error = nil, want refusal")
			}
		})
	}
}
