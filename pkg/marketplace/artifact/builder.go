package artifact

import (
	"bytes"
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// BuildResult is the unified output of building a resource or collection artifact.
type BuildResult struct {
	Artifact   hccartifact.CanonicalArtifact
	RawArchive []byte
	// ManifestPayload and ManifestDigest are populated for virtual collection
	// builds. The standalone manifest is the collection revision identity; the
	// tar archive remains available for the legacy source-revision artifact.
	ManifestPayload []byte
	ManifestDigest  string
	ContentDigest   string
	ArchiveDigest   string
	RevisionDigest  string
	Findings        []analyze.Finding
	Capabilities    []string
	Name            string
	Kind            string
	License         string
	Rejected        bool
	RejectReason    string
}

// BuildResourceArtifact analyzes a resource, extracts capabilities, and produces a
// canonical tar.gz artifact. If the analyzer rejects the resource, no artifact
// or raw archive is emitted.
func BuildResourceArtifact(ctx context.Context, r ir.Resource, files analyze.FileSet, opts analyze.Options) (BuildResult, error) {
	if r == nil {
		return BuildResult{}, fmt.Errorf("nil resource")
	}

	caps := ExtractCapabilities(r)

	base, err := hccartifact.FromIR(r)
	if err != nil {
		return BuildResult{}, fmt.Errorf("from IR: %w", err)
	}

	engine := analyze.NewEngine()
	report, err := engine.Analyze(ctx, r, files, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("analyze: %w", err)
	}

	if report.Rejected {
		return BuildResult{
			Name:          base.Manifest.Name,
			Kind:          base.Manifest.Kind,
			License:       opts.Source.License,
			ContentDigest: base.Manifest.ContentDigest,
			Findings:      report.Findings,
			Capabilities:  caps,
			Rejected:      true,
			RejectReason:  "policy rejected one or more findings",
		}, nil
	}

	base.Manifest.AnalyzerVersion = analyzerVersion(opts)
	base.Manifest.Capabilities = caps
	base.Manifest.Findings = ToCanonicalFindings(report.Findings)
	base.Manifest.License = opts.Source.License
	base.Manifest.Provenance = provenanceFromSource(opts)
	base.Manifest.Revision = opts.Source.Ref
	base.Manifest.SourceID = opts.Source.ID
	base.Manifest.SourceSlug = opts.Source.Slug

	raw, digest, err := hccartifact.Encode(base)
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode artifact: %w", err)
	}

	// Decode the raw archive to obtain an artifact whose rawArchive field is
	// populated, so downstream ValidateArchive/VerifyArchiveDigest calls work.
	artifact, _, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return BuildResult{}, fmt.Errorf("decode artifact: %w", err)
	}

	revisionDigest, err := buildRevisionDigest(r, base.Manifest.ContentDigest, digest, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("revision digest: %w", err)
	}

	return BuildResult{
		Artifact:       artifact,
		Name:           artifact.Manifest.Name,
		Kind:           artifact.Manifest.Kind,
		License:        base.Manifest.License,
		RawArchive:     raw,
		ContentDigest:  base.Manifest.ContentDigest,
		ArchiveDigest:  digest,
		RevisionDigest: revisionDigest,
		Findings:       report.Findings,
		Capabilities:   caps,
		Rejected:       false,
	}, nil
}

// BuildCollectionManifest builds a standalone collection manifest and keeps an
// in-memory canonical envelope for metadata consumers. It never produces a tar
// archive. If memberResults is provided, a source revision manifest is also
// produced and its digest is returned in BuildResult.RevisionDigest.
func BuildCollectionManifest(ctx context.Context, c *ir.Collection, memberResults []BuildResult, opts analyze.Options) (BuildResult, error) {
	if c == nil {
		return BuildResult{}, fmt.Errorf("nil collection")
	}

	engine := analyze.NewEngine()
	report, err := engine.Analyze(ctx, c, nil, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("analyze collection: %w", err)
	}

	caps := ExtractCapabilities(c)

	if report.Rejected {
		return BuildResult{
			Findings:     report.Findings,
			Capabilities: caps,
			Rejected:     true,
			RejectReason: "policy rejected one or more collection findings",
		}, nil
	}

	manifest, err := hccartifact.CollectionFromIR(c)
	if err != nil {
		return BuildResult{}, fmt.Errorf("collection from IR: %w", err)
	}
	manifest.Provenance = provenanceFromSource(opts)
	if manifest.SupportedSchemas == nil {
		manifest.SupportedSchemas = []string{hccartifact.CollectionSchemaV1}
	}
	for _, member := range memberResults {
		if member.Rejected {
			return BuildResult{}, fmt.Errorf("collection member %s/%q is rejected and has no publishable leaf artifact", member.Kind, member.Name)
		}
	}
	if len(memberResults) > 0 {
		manifest = resolveCollectionManifestMembers(manifest, memberResults, opts)
	}
	// Collections are virtual: only the standalone collection manifest and its
	// exact leaf artifacts are published. Validate the publishable flat slice
	// before encoding anything that can be handed to a publisher.
	if err := hccartifact.ValidateFlatMirrorManifest(manifest); err != nil {
		return BuildResult{}, fmt.Errorf("validate flat collection manifest: %w", err)
	}

	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(manifest)
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode collection manifest: %w", err)
	}

	// Keep an in-memory canonical envelope for callers that need collection
	// metadata, but deliberately do not encode it as a tar archive. A collection
	// resource revision is identified by the standalone manifest digest, not by
	// an archive digest.
	artifact := hccartifact.CanonicalArtifact{
		Manifest: hccartifact.CanonicalManifest{
			SchemaVersion:    hccartifact.CanonicalArtifactSchemaV1,
			ArtifactVersion:  hccartifact.CanonicalArtifactSchemaV1,
			Kind:             "collection",
			Name:             c.Name(),
			Revision:         opts.Source.Ref,
			ContentDigest:    manifestDigest,
			License:          opts.Source.License,
			AnalyzerVersion:  analyzerVersion(opts),
			Capabilities:     caps,
			Findings:         ToCanonicalFindings(report.Findings),
			Provenance:       provenanceFromSource(opts),
			SupportedSchemas: []string{hccartifact.CanonicalArtifactSchemaV1},
		},
		Payload: payload,
	}

	revisionDigest := ""
	if len(memberResults) > 0 {
		revManifest := SourceRevisionManifest{
			SchemaVersion: SourceRevisionSchemaV1,
			Generator:     fmt.Sprintf("agent-marketplace source-revision v%s", analyzerVersion(opts)),
		}
		for _, mr := range memberResults {
			kind := mr.Kind
			if kind == "" {
				kind = memberKind(mr)
			}
			name := mr.Name
			if name == "" && mr.Artifact.Manifest.Name != "" {
				name = mr.Artifact.Manifest.Name
			}
			revManifest.Resources = append(revManifest.Resources, RevisionResource{
				Kind:          kind,
				Name:          name,
				ContentDigest: mr.ContentDigest,
				ArchiveDigest: mr.ArchiveDigest,
				Rejected:      mr.Rejected,
			})
		}
		_, digest, err := EncodeSourceRevisionManifest(revManifest)
		if err != nil {
			return BuildResult{}, fmt.Errorf("encode revision manifest: %w", err)
		}
		revisionDigest = digest
	}

	return BuildResult{
		Artifact:        artifact,
		Name:            c.Name(),
		Kind:            "collection",
		License:         artifact.Manifest.License,
		ManifestPayload: payload,
		ManifestDigest:  manifestDigest,
		ContentDigest:   manifestDigest,
		RevisionDigest:  revisionDigest,
		Findings:        report.Findings,
		Capabilities:    caps,
		Rejected:        false,
	}, nil
}

func analyzerVersion(opts analyze.Options) string {
	if opts.Version != "" {
		return opts.Version
	}
	return analyze.AnalyzerVersionV1
}

func provenanceFromSource(opts analyze.Options) hccartifact.CanonicalProvenance {
	return hccartifact.CanonicalProvenance{
		RegistryURL: opts.Source.RegistryURL,
		Ref:         opts.Source.Ref,
		Path:        opts.Source.Path,
		Generator:   fmt.Sprintf("agent-marketplace artifact v%s", analyzerVersion(opts)),
	}
}

func buildRevisionDigest(r ir.Resource, contentDigest, archiveDigest string, opts analyze.Options) (string, error) {
	if r == nil {
		return "", nil
	}
	rev := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     fmt.Sprintf("agent-marketplace source-revision v%s", analyzerVersion(opts)),
		Resources: []RevisionResource{{
			Kind:          r.Kind().String(),
			Name:          r.Name(),
			ContentDigest: contentDigest,
			ArchiveDigest: archiveDigest,
			Rejected:      false,
		}},
	}
	_, digest, err := EncodeSourceRevisionManifest(rev)
	if err != nil {
		return "", err
	}
	return digest, nil
}

func resolveCollectionManifestMembers(manifest hccartifact.CollectionManifest, memberResults []BuildResult, opts analyze.Options) hccartifact.CollectionManifest {
	if len(memberResults) == 0 {
		return manifest
	}
	byKey := make(map[string]BuildResult, len(memberResults))
	for _, mr := range memberResults {
		kind := mr.Kind
		if kind == "" {
			kind = memberKind(mr)
		}
		key := kind + "/" + mr.Name
		byKey[key] = mr
	}

	resolved := make([]hccartifact.CollectionMemberEntry, 0, len(memberResults))
	for i := range manifest.Members {
		m := manifest.Members[i]
		key := m.Kind + "/" + m.Name
		if mr, ok := byKey[key]; ok {
			m.SourceID = opts.Source.ID
			m.Revision = mr.ContentDigest
			delete(byKey, key)
		}
		resolved = append(resolved, m)
	}

	for _, mr := range memberResults {
		kind := mr.Kind
		if kind == "" {
			kind = memberKind(mr)
		}
		key := kind + "/" + mr.Name
		if _, ok := byKey[key]; !ok {
			continue
		}
		resolved = append(resolved, hccartifact.CollectionMemberEntry{
			Kind:     kind,
			Name:     mr.Name,
			SourceID: opts.Source.ID,
			Revision: mr.ContentDigest,
		})
	}

	if len(resolved) > len(manifest.Members) {
		// Includes were expanded into the member list; drop unresolved names.
		manifest.Includes = nil
	}
	manifest.Members = resolved
	return manifest
}

func memberKind(mr BuildResult) string {
	if mr.Artifact.Manifest.Kind != "" {
		return mr.Artifact.Manifest.Kind
	}
	for _, c := range mr.Capabilities {
		if c == "skill" || c == "command" || c == "subagent" || c == "hook" || c == "mcp" || c == "collection" {
			return c
		}
	}
	return "resource"
}
