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
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "id", Slug: "slug"}

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

	s := ir.NewSkill("review", "Review", "\u202Ehidden", nil, nil, ir.SourceRef{})

	result, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}
	if !result.Rejected {
		t.Fatal("expected rejected artifact")
	}
	if len(result.RawArchive) == 0 || result.ArchiveDigest == "" {
		t.Fatal("rejected resource should still have its intrinsic archive")
	}
}

func TestBuildResourceArtifactArchiveIsAnalyzerIndependent(t *testing.T) {
	s := ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}
	files := analyze.FileSet{"run.sh": []byte("#!/bin/sh\necho hi\n")}

	warnOpts := analyze.DefaultOptions()
	warnOpts.Version = "analyzer-v1"
	warnOpts.Registry = analyze.Registry{RegistryURL: "https://example.com/one.git", Ref: "main", Path: "skills/review", ID: "one", Slug: "one", License: "MIT"}
	warn, err := BuildResourceArtifact(context.Background(), s, files, warnOpts)
	if err != nil {
		t.Fatal(err)
	}
	if warn.Rejected || len(warn.Findings) == 0 {
		t.Fatalf("expected accepted resource with findings: rejected=%v findings=%v", warn.Rejected, warn.Findings)
	}

	rejectOpts := analyze.DefaultOptions()
	rejectOpts.Version = "analyzer-v2"
	rejectOpts.Policy = analyze.Policy{Name: "strict", Outcomes: map[string]string{analyze.FindingExecutableScript: analyze.OutcomeReject}}
	rejectOpts.Registry = analyze.Registry{RegistryURL: "https://example.com/two.git", Ref: "release", Path: "other/review", ID: "two", Slug: "two", License: "Apache-2.0"}
	rejected, err := BuildResourceArtifact(context.Background(), s, files, rejectOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !rejected.Rejected || len(rejected.Findings) == 0 {
		t.Fatalf("expected rejected resource with findings: rejected=%v findings=%v", rejected.Rejected, rejected.Findings)
	}

	if !bytes.Equal(warn.RawArchive, rejected.RawArchive) || warn.ArchiveDigest != rejected.ArchiveDigest {
		t.Fatalf("analyzer changed intrinsic archive: warn=%q rejected=%q", warn.ArchiveDigest, rejected.ArchiveDigest)
	}
	manifest := rejected.Artifact.Manifest
	if manifest.AnalyzerVersion != "" || len(manifest.Findings) != 0 {
		t.Fatalf("canonical manifest contains analyzer output: %#v", manifest)
	}
	if manifest.RegistryID != "" || manifest.RegistrySlug != "" || manifest.Revision != "" || manifest.Provenance.RegistryURL != "" || manifest.Provenance.Ref != "" || manifest.Provenance.Path != "" {
		t.Fatalf("canonical manifest contains occurrence provenance: %#v", manifest)
	}
}

func TestBuildResourceArtifactAuditsAndDecision(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "id", Slug: "slug"}

	s := ir.NewSkill("review", "Review", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	result, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}
	if result.Decision == "" {
		t.Fatal("expected Decision on BuildResult")
	}
	if len(result.Audits) == 0 {
		t.Fatal("expected Audits on BuildResult")
	}
	if result.Audits[len(result.Audits)-1].AnalyzerID != analyze.AnalyzerCapabilities {
		t.Fatalf("expected capabilities audit last, got %q", result.Audits[len(result.Audits)-1].AnalyzerID)
	}
}

func TestBuildCollectionManifest(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "registry-id"}

	s := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	skillResult, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}

	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
	}, nil, nil, ir.SourceRef{})

	result, err := BuildCollectionManifest(ctx, c, []BuildResult{skillResult}, opts)
	if err != nil {
		t.Fatalf("BuildCollectionManifest: %v", err)
	}
	if result.Rejected {
		t.Fatalf("collection rejected: %v", result.Findings)
	}
	if result.ContentDigest == "" || result.ManifestDigest == "" || result.ContentDigest != result.ManifestDigest {
		t.Fatalf("collection manifest digest = content %q, manifest %q", result.ContentDigest, result.ManifestDigest)
	}
	if len(result.RawArchive) != 0 || result.ArchiveDigest != "" {
		t.Fatalf("virtual collection unexpectedly has archive: bytes=%d digest=%q", len(result.RawArchive), result.ArchiveDigest)
	}
	if result.RevisionDigest == "" {
		t.Fatal("expected revision digest")
	}

	// The standalone manifest payload is JSON, not a collection tar artifact.
	if !bytes.Contains(result.ManifestPayload, []byte(`"schema_version"`)) || !bytes.Equal(result.ManifestPayload, result.Artifact.Payload) {
		t.Fatal("collection payload missing or not equal to standalone manifest")
	}
}

func TestBuildCollectionManifestNil(t *testing.T) {
	_, err := BuildCollectionManifest(context.Background(), nil, nil, analyze.DefaultOptions())
	if err == nil {
		t.Fatal("expected error for nil collection")
	}
}

func TestBuildResourceArtifactExplicitEmptyRegistrations(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Registrations = []analyze.Registration{}
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "id", Slug: "slug"}

	s := ir.NewSkill("review", "Review", "Review <!-- hidden --> carefully, then run eval(code).", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	result, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}
	if len(result.Audits) != 0 {
		t.Fatalf("explicit empty registrations should yield zero audits, got %d", len(result.Audits))
	}
	if len(result.Findings) != 0 {
		t.Fatalf("explicit empty registrations should yield zero findings, got %d", len(result.Findings))
	}
	if result.Decision != analyze.DecisionAllow {
		t.Fatalf("explicit empty registrations should allow, got %q", result.Decision)
	}
	if result.Rejected {
		t.Fatal("explicit empty registrations should not reject")
	}
	if len(result.RawArchive) == 0 {
		t.Fatal("expected raw archive with zero analyzers")
	}
}

func TestBuildCollectionManifestExplicitEmptyRegistrations(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Registrations = []analyze.Registration{}
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "registry-id"}

	s := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	skillResult, err := BuildResourceArtifact(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact: %v", err)
	}

	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
	}, nil, nil, ir.SourceRef{})

	result, err := BuildCollectionManifest(ctx, c, []BuildResult{skillResult}, opts)
	if err != nil {
		t.Fatalf("BuildCollectionManifest: %v", err)
	}
	if len(result.Audits) != 0 {
		t.Fatalf("explicit empty registrations should yield zero collection audits, got %d", len(result.Audits))
	}
	if len(result.Findings) != 0 {
		t.Fatalf("explicit empty registrations should yield zero collection findings, got %d", len(result.Findings))
	}
	if result.Decision != analyze.DecisionAllow {
		t.Fatalf("explicit empty registrations should allow collection, got %q", result.Decision)
	}
	if result.Rejected {
		t.Fatal("explicit empty registrations should not reject collection")
	}
	if len(result.ManifestPayload) == 0 {
		t.Fatal("expected collection manifest payload with zero analyzers")
	}
}

func TestBuildCollectionManifestIgnoresMemberAssessment(t *testing.T) {
	ctx := context.Background()
	opts := analyze.DefaultOptions()
	opts.Registry = analyze.Registry{RegistryURL: "https://example.com/repo.git", Ref: "main", ID: "registry-id"}

	acceptedSkill := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	accepted, err := BuildResourceArtifact(ctx, acceptedSkill, nil, opts)
	if err != nil {
		t.Fatalf("BuildResourceArtifact accepted: %v", err)
	}
	if accepted.Rejected {
		t.Fatalf("expected accepted skill, got rejected: %v", accepted.Findings)
	}

	rejectedSkill := ir.NewSkill("bad", "Bad", "\u202Ehidden", nil, nil, ir.SourceRef{})
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

	collection, err := BuildCollectionManifest(ctx, c, []BuildResult{accepted, rejected}, opts)
	if err != nil {
		t.Fatalf("BuildCollectionManifest: %v", err)
	}
	if !bytes.Contains(collection.ManifestPayload, []byte(`"name":"bad"`)) {
		t.Fatalf("collection identity omitted assessed member: %s", collection.ManifestPayload)
	}
	if !collection.Rejected || len(collection.Findings) == 0 {
		t.Fatalf("collection assessment did not reflect rejected member: %#v", collection)
	}
}
