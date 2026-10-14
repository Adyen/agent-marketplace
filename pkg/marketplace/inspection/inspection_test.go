package inspection

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-api/active-url-schemes-are-metadata-only
func TestBuildHidesActiveSchemeSupport(t *testing.T) {
	for _, body := range []string{"javascript:alert(1)", "data:text/plain,active", "vbscript:MsgBox(1)", "  DaTa:text/html,active  ", "VBSCRIPT:active"} {
		t.Run(body, func(t *testing.T) {
			projection, err := Build(store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), []byte("# Review\n"), map[string][]byte{"unsafe.txt": []byte(body), "safe.txt": []byte("safe notes")}, 10, 1024, 4096)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range projection.Manifest.Files {
				if file.Path == "support/unsafe.txt" && (file.InspectionClass != ClassMetadata || len(projection.Bodies[file.ContentDigest]) != 0) {
					t.Fatalf("active support exposed: %#v", file)
				}
				if file.Path == "support/safe.txt" && string(projection.Bodies[file.ContentDigest]) != "safe notes" {
					t.Fatal("safe support control was not exposed")
				}
			}
			if len(projection.Manifest.Files) != 3 {
				t.Fatalf("missing inspection files: %#v", projection.Manifest.Files)
			}
		})
	}
}

func TestBuildCanonicalSkillProjection(t *testing.T) {
	body := []byte("# Review\nRead carefully.\n")
	support := map[string][]byte{"notes.md": []byte("notes")}
	projection, err := Build(store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), body, support, 10, 1024, 4096)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if projection.Manifest.PrimaryPath != "SKILL.md" || len(projection.Manifest.Files) != 2 {
		t.Fatalf("manifest = %#v", projection.Manifest)
	}
	if bytes.Contains(projection.Payload, []byte(`"findings"`)) {
		t.Fatalf("inspection payload contains analyzer findings: %s", projection.Payload)
	}
	if err := Verify(projection.Manifest, projection.Payload, projection.Manifest.ManifestDigest); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if string(projection.Bodies[Digest(body)]) != string(body) {
		t.Fatalf("primary body missing from digest map")
	}
}

func TestBuildProjectionIsAnalyzerIndependent(t *testing.T) {
	// Build deliberately has no analyzer version, findings, or policy-decision
	// input: the inspection identity is a projection of files only.
	buildFilesOnly := Build
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
	revision := "sha256:" + strings.Repeat("a", 64)
	body := []byte("# Review\nRead carefully.\n")
	first, err := buildFilesOnly(coordinate, revision, body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildFilesOnly(coordinate, revision, body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Payload, second.Payload) || first.Manifest.ManifestDigest != second.Manifest.ManifestDigest {
		t.Fatalf("file projection is unstable: first=%q second=%q", first.Manifest.ManifestDigest, second.Manifest.ManifestDigest)
	}
}

func TestBuildRejectsBoundsAndHidesSVGBody(t *testing.T) {
	if _, err := Build(store.ResourceCoordinate{Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), []byte("body"), nil, 1, 3, 100); err == nil {
		t.Fatal("oversized primary content accepted")
	}
	projection, err := Build(store.ResourceCoordinate{Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), []byte("body"), map[string][]byte{"diagram.svg": []byte("<svg></svg>")}, 3, 100, 100)
	if err != nil {
		t.Fatalf("Build SVG: %v", err)
	}
	for _, file := range projection.Manifest.Files {
		if file.Path == "support/diagram.svg" {
			if file.InspectionClass != ClassMetadata || len(projection.Bodies[file.ContentDigest]) != 0 {
				t.Fatalf("SVG policy = %#v", file)
			}
			return
		}
	}
	t.Fatal("SVG metadata missing")
}

func TestBuildProducesCanonicalProjectionForEveryMarketplaceKind(t *testing.T) {
	kinds := []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			body := []byte("# " + kind + "\nSafe content.\n")
			if kind == "mcpserver" || kind == "collection" {
				body = []byte(`{"name":"safe","enabled":true}`)
			}
			support := map[string][]byte{
				"notes.txt":   []byte("exact UTF-8 support bytes"),
				"diagram.svg": []byte("<svg><script>alert(1)</script></svg>"),
				"image.png":   {0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
				"opaque.bin":  {0x00, 0xff, 0x01},
			}
			coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: kind, Name: "example"}
			projection, err := Build(coordinate, "sha256:"+strings.Repeat("a", 64), body, support, 10, 1024, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if projection.Manifest.PrimaryPath != PrimaryPath(kind) || len(projection.Manifest.Files) != 5 {
				t.Fatalf("manifest = %#v", projection.Manifest)
			}
			if err := Verify(projection.Manifest, projection.Payload, projection.Manifest.ManifestDigest); err != nil {
				t.Fatal(err)
			}
			for _, file := range projection.Manifest.Files {
				if file.Path == "support/diagram.svg" || file.Path == "support/opaque.bin" {
					if file.InspectionClass != ClassMetadata || len(projection.Bodies[file.ContentDigest]) != 0 {
						t.Fatalf("unsafe body exposed: %#v", file)
					}
				}
			}
		})
	}
}

func TestVerifyRequiresCanonicalPrimaryMediaAndReferencedBodyPolicy(t *testing.T) {
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "mcpserver", Name: "remote"}
	body := []byte(`{"name":"remote"}`)
	projection, err := Build(coordinate, "sha256:"+strings.Repeat("a", 64), body, map[string][]byte{"notes.md": []byte("notes")}, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	manifest := projection.Manifest
	manifest.Files[0].MediaType = MediaPlain
	payload, err := canonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(manifest, payload, ""); err == nil {
		t.Fatal("non-canonical primary media accepted")
	}

	file := projection.Manifest.Files[1]
	file.MediaType = MediaPlain
	if err := VerifyFileContent(file, projection.Bodies[file.ContentDigest]); err == nil {
		t.Fatal("support body with mismatched media accepted")
	}

	projection, err = Build(store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("b", 64), []byte("# review\n"), map[string][]byte{"notes.md": []byte("# notes\n")}, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range projection.Manifest.Files {
		if candidate.Path == "support/notes.md" {
			if err := VerifyFileContent(candidate, projection.Bodies[candidate.ContentDigest]); err != nil {
				t.Fatalf("safe support Markdown rejected: %v", err)
			}
		}
	}
}

func TestBuildRejectsUnsafeAndDuplicateSupportPathsAndManifestBounds(t *testing.T) {
	coordinate := store.ResourceCoordinate{Kind: "skill", Name: "review"}
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, support := range []map[string][]byte{
		{"../notes.txt": []byte("x")},
		{"notes/../notes.txt": []byte("x")},
		{"notes.txt": []byte("x"), "./notes.txt": []byte("y")},
		{"COMMAND.md": []byte("x")},
	} {
		if _, err := Build(coordinate, digest, []byte("body"), support, 10, 100, 100, 10000); err == nil {
			t.Fatalf("unsafe or duplicate support paths accepted: %#v", support)
		}
	}
	if _, err := Build(coordinate, digest, []byte("body"), map[string][]byte{strings.Repeat("x", 256) + ".txt": []byte("x")}, 10, 1000, 1000, 32); err == nil {
		t.Fatal("oversized serialized inspection manifest accepted")
	}
}
