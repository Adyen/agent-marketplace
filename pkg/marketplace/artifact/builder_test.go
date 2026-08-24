package artifact

import (
	"bytes"
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

func TestBuildResourceArtifact(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Source = analyze.Source{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "id", Slug: "slug"}

	s := ir.NewSkill("review", "Review", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	files := analyze.FileSet{
		"README.md": []byte("# Review\n"),
	}

	result, err := BuildResourceArtifact(ctx, s, files, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}
	if result.Rejected {
		t.Fatalf("expected artifact to be accepted: %v", result.Findings)
	}
	if len(result.RawArchive) == 0 {
		t.Fatal("expected raw archive")
	}
	if result.ContentDigest == "" || result.ArchiveDigest == "" {
		t.Fatal("expected content and archive digests")
	}
	if result.RevisionDigest == "" {
		t.Fatal("expected revision digest")
	}
	if len(result.Capabilities) == 0 {
		t.Fatal("expected capabilities")
	}
	if len(result.Artifact.Payload) == 0 {
		t.Fatal("expected artifact payload")
	}

	if err := result.Artifact.ValidateArchive(nil); err != nil {
		t.Fatalf("ValidateArchive: %v", err)
	}
	if err := result.Artifact.VerifyArchiveDigest(result.RawArchive); err != nil {
		t.Fatalf("VerifyArchiveDigest: %v", err)
	}
}

func TestBuildResourceArtifactRejects(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()

	s := ir.NewSkill("review", "Review", "<!-- hidden -->", nil, nil, ir.SourceRef{})

	result, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}
	if !result.Rejected {
		t.Fatal("expected rejected artifact")
	}
	if len(result.RawArchive) > 0 {
		t.Fatal("rejected artifact should not have raw archive")
	}
}

func TestBuildCollectionManifest(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Source = analyze.Source{RegistryURL: "https://example.com/repo.git", Ref: "main"}

	s := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	skillResult, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}

	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
	}, []string{"base"}, nil, ir.SourceRef{})

	result, err := BuildCollectionManifest(ctx, c, []BuildResult{skillResult}, opts)
	if err != nil {
		t.Fatalf("BuildCollectionManifest: %v", err)
	}
	if result.Rejected {
		t.Fatalf("collection rejected: %v", result.Findings)
	}
	if result.ContentDigest == "" {
		t.Fatal("expected content digest")
	}
	if len(result.RawArchive) == 0 {
		t.Fatal("expected raw archive")
	}
	if result.RevisionDigest == "" {
		t.Fatal("expected revision digest")
	}

	if err := result.Artifact.ValidateArchive(nil); err != nil {
		t.Fatalf("ValidateArchive: %v", err)
	}

	// The artifact payload should be a valid collection manifest JSON.
	if !bytes.Contains(result.Artifact.Payload, []byte(`"schema_version"`)) {
		t.Fatal("collection payload missing schema_version")
	}
}

func TestBuildCollectionManifestNil(t *testing.T) {
	_, err := BuildCollectionManifest(context.Background(), nil, nil, analyze.DefaultOptions())
	if err == nil {
		t.Fatal("expected error for nil collection")
	}
}

func TestBuildCollectionManifestWithRejectedMember(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Source = analyze.Source{RegistryURL: "https://example.com/repo.git", Ref: "main"}

	acceptedSkill := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	accepted, err := BuildResourceArtifact(ctx, acceptedSkill, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact accepted: %v", err)
	}
	if accepted.Rejected {
		t.Fatalf("expected accepted skill, got rejected: %v", accepted.Findings)
	}

	rejectedSkill := ir.NewSkill("bad", "Bad", "<!-- hidden -->", nil, nil, ir.SourceRef{})
	rejected, err := BuildResourceArtifact(ctx, rejectedSkill, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact rejected: %v", err)
	}
	if !rejected.Rejected {
		t.Fatal("expected skill to be rejected")
	}
	if rejected.Kind != "skill" || rejected.Name != "bad" {
		t.Fatalf("rejected result missing kind/name: got %q/%q", rejected.Kind, rejected.Name)
	}

	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
		{Kind: ir.KindSkill, Name: "bad"},
	}, nil, nil, ir.SourceRef{})

	result, err := BuildCollectionManifest(ctx, c, []BuildResult{accepted, rejected}, opts)
	if err != nil {
		t.Fatalf("BuildCollectionManifest: %v", err)
	}
	if result.Rejected {
		t.Fatalf("collection rejected: %v", result.Findings)
	}
	if result.RevisionDigest == "" {
		t.Fatal("expected revision digest")
	}
	if err := result.Artifact.ValidateArchive(nil); err != nil {
		t.Fatalf("ValidateArchive: %v", err)
	}

	// Reconstruct the source revision manifest to verify the rejected member is
	// represented correctly.
	rev := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     "agent-marketplace source-revision v" + analyzerVersion(opts),
		Resources: []RevisionResource{
			{
				Kind:          accepted.Kind,
				Name:          accepted.Name,
				ContentDigest: accepted.ContentDigest,
				ArchiveDigest: accepted.ArchiveDigest,
				Rejected:      false,
			},
			{
				Kind:          rejected.Kind,
				Name:          rejected.Name,
				ContentDigest: rejected.ContentDigest,
				ArchiveDigest: rejected.ArchiveDigest,
				Rejected:      true,
			},
		},
	}
	_, expectedDigest, err := EncodeSourceRevisionManifest(rev)
	if err != nil {
		t.Fatalf("EncodeSourceRevisionManifest: %v", err)
	}
	if expectedDigest != result.RevisionDigest {
		t.Fatalf("revision digest mismatch: expected %s, got %s", expectedDigest, result.RevisionDigest)
	}
}
