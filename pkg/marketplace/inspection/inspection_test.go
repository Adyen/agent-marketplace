package inspection

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestBuildCanonicalSkillProjection(t *testing.T) {
	body := []byte("# Review\nRead carefully.\n")
	support := map[string][]byte{"notes.md": []byte("notes")}
	projection, err := Build(store.ResourceCoordinate{Marketplace: "mp", Source: "source", Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), body, support, []analyze.Finding{{Code: "finding", Severity: analyze.OutcomeWarn, PolicyOutcome: analyze.OutcomeWarn, Message: "check", SourceRange: analyze.SourceRangeAt("skill/review/body", string(body), 2)}}, 10, 1024, 4096)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if projection.Manifest.PrimaryPath != "SKILL.md" || len(projection.Manifest.Files) != 2 {
		t.Fatalf("manifest = %#v", projection.Manifest)
	}
	if projection.Manifest.Findings[0].Path != "SKILL.md" || projection.Manifest.Findings[0].EndByte <= projection.Manifest.Findings[0].StartByte {
		t.Fatalf("finding range = %#v", projection.Manifest.Findings[0])
	}
	if err := Verify(projection.Manifest, projection.Payload, projection.Manifest.ManifestDigest); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if string(projection.Bodies[Digest(body)]) != string(body) {
		t.Fatalf("primary body missing from digest map")
	}
}

func TestBuildRejectsBoundsAndHidesSVGBody(t *testing.T) {
	if _, err := Build(store.ResourceCoordinate{Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), []byte("body"), nil, nil, 1, 3, 100); err == nil {
		t.Fatal("oversized primary content accepted")
	}
	projection, err := Build(store.ResourceCoordinate{Kind: "skill", Name: "review"}, "sha256:"+strings.Repeat("a", 64), []byte("body"), map[string][]byte{"diagram.svg": []byte("<svg></svg>")}, nil, 3, 100, 100)
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

func TestBuildRejectsUnsafeAndDuplicateSupportPathsAndManifestBounds(t *testing.T) {
	coordinate := store.ResourceCoordinate{Kind: "skill", Name: "review"}
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, support := range []map[string][]byte{
		{"../notes.txt": []byte("x")},
		{"notes/../notes.txt": []byte("x")},
		{"notes.txt": []byte("x"), "./notes.txt": []byte("y")},
	} {
		if _, err := Build(coordinate, digest, []byte("body"), support, nil, 10, 100, 100, 10000); err == nil {
			t.Fatalf("unsafe or duplicate support paths accepted: %#v", support)
		}
	}
	if _, err := Build(coordinate, digest, []byte("body"), nil, []analyze.Finding{{Code: strings.Repeat("x", 32), Message: strings.Repeat("y", 256)}}, 10, 1000, 1000, 32); err == nil {
		t.Fatal("oversized serialized inspection manifest accepted")
	}
}
