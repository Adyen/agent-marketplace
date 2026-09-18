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
	// tar archive remains available for the registry-revision artifact.
	ManifestPayload []byte
	ManifestDigest  string
	ContentDigest   string
	ArchiveDigest   string
	RevisionDigest  string
	Findings        []analyze.Finding
	Audits          []analyze.Audit
	Decision        string
	Capabilities    []string
	Name            string
	Kind            string
	License         string
	// Description is the author-written one-line summary, normalized and
	// bounded by store.BoundDescription. It is populated for rejected results
	// too: a rejected resource stays listed for transparency and still needs a
	// description to be identifiable.
	Description  string
	Rejected     bool
	RejectReason string
}

// engineFromOptions selects the analyzer engine. A non-nil Registrations slice
// on Options takes precedence, so an explicit empty list installs zero
// analyzers. A nil slice falls back to the default built-in suite with the
// configured policy applied to every built-in analyzer.
func engineFromOptions(opts analyze.Options) *analyze.Engine {
	if opts.Registrations != nil {
		return analyze.NewEngineWith(opts.Registrations)
	}
	regs := analyze.DefaultRegistrations()
	if opts.Policy.Name != "" || opts.Policy.Outcomes != nil {
		for i := range regs {
			regs[i].Policy = opts.Policy
		}
	}
	return analyze.NewEngineWith(regs)
}

// runArtifactAnalyze invokes the configured analyzer adapter or the selected
// Engine. The content digest is computed before analysis so cache-aware adapters
// can decide which registrations to execute before the BuildResult is finalized.
func runArtifactAnalyze(ctx context.Context, subject analyze.Subject, contentDigest string, opts analyze.Options) (analyze.Report, error) {
	if opts.Analyze != nil {
		return opts.Analyze(ctx, subject, contentDigest)
	}
	if opts.Registrations != nil && len(opts.Registrations) == 0 {
		return analyze.Report{Decision: analyze.DecisionAllow, Rejected: false, Audits: []analyze.Audit{}, Findings: []analyze.Finding{}}, nil
	}
	return engineFromOptions(opts).Analyze(ctx, subject)
}

// BuildResourceArtifact analyzes a resource, extracts capabilities, and produces a
// canonical tar.gz artifact. Analyzer findings and policy decisions are returned
// alongside, but are not embedded in or allowed to suppress the intrinsic archive.
func BuildResourceArtifact(ctx context.Context, r ir.Resource, files analyze.FileSet, opts analyze.Options) (BuildResult, error) {
	if r == nil {
		return BuildResult{}, fmt.Errorf("nil resource")
	}

	caps := ExtractCapabilities(r)
	description := ExtractDescription(r)

	base, err := hccartifact.FromIR(r)
	if err != nil {
		return BuildResult{}, fmt.Errorf("from IR: %w", err)
	}

	// Only properties derived from the resource belong in the canonical archive.
	// Analyzer output, policy decisions, and registry occurrence metadata are stored
	// separately by Marketplace so re-analysis cannot change resource identity.
	base.Manifest.Capabilities = caps

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

	subject := analyze.Subject{
		Resource: r,
		Files:    files,
		Registry: opts.Registry,
		Limits:   opts.Limits,
	}
	contentDigest := base.Manifest.ContentDigest
	report, err := runArtifactAnalyze(ctx, subject, contentDigest, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("analyze: %w", err)
	}

	revisionDigest, err := buildRevisionDigest(r, contentDigest, digest, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("revision digest: %w", err)
	}

	rejectReason := ""
	if report.Rejected {
		rejectReason = "policy rejected one or more findings"
	}
	return BuildResult{
		Artifact:       artifact,
		Name:           artifact.Manifest.Name,
		Kind:           artifact.Manifest.Kind,
		License:        opts.Registry.License,
		Description:    description,
		RawArchive:     raw,
		ContentDigest:  contentDigest,
		ArchiveDigest:  digest,
		RevisionDigest: revisionDigest,
		Findings:       report.Findings,
		Audits:         report.Audits,
		Decision:       report.Decision,
		Capabilities:   caps,
		Rejected:       report.Rejected,
		RejectReason:   rejectReason,
	}, nil
}

// BuildCollectionManifest builds a standalone collection manifest and keeps an
// in-memory canonical envelope for metadata consumers. It never produces a tar
// archive. If memberResults is provided, a registry revision manifest is also
// produced and its digest is returned in BuildResult.RevisionDigest.
func BuildCollectionManifest(ctx context.Context, c *ir.Collection, memberResults []BuildResult, opts analyze.Options) (BuildResult, error) {
	if c == nil {
		return BuildResult{}, fmt.Errorf("nil collection")
	}

	caps := ExtractCapabilities(c)
	description := ExtractDescription(c)

	manifest, err := hccartifact.CollectionFromIR(c)
	if err != nil {
		return BuildResult{}, fmt.Errorf("collection from IR: %w", err)
	}
	if manifest.SupportedSchemas == nil {
		manifest.SupportedSchemas = []string{hccartifact.CollectionSchemaV1}
	}

	seenMemberRevisions := make(map[string]string, len(memberResults))
	for _, member := range memberResults {
		kind := member.Kind
		if kind == "" {
			kind = memberKind(member)
		}
		key := kind + "/" + member.Name
		if previous, exists := seenMemberRevisions[key]; exists && previous != member.ContentDigest {
			return BuildResult{}, fmt.Errorf("collection contains conflicting revisions of %s/%q", kind, member.Name)
		}
		seenMemberRevisions[key] = member.ContentDigest
	}
	if len(memberResults) > 0 {
		manifest = resolveCollectionManifestMembers(manifest, memberResults, opts)
	}
	if err := hccartifact.ValidateCollectionManifest(manifest); err != nil {
		return BuildResult{}, fmt.Errorf("validate collection manifest: %w", err)
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
			ContentDigest:    manifestDigest,
			Capabilities:     caps,
			SupportedSchemas: []string{hccartifact.CanonicalArtifactSchemaV1},
		},
		Payload: payload,
	}

	// Analyzer execution runs after the canonical manifest/content digest exists
	// so cache-aware adapters can key on the stable collection identity.
	subject := analyze.Subject{
		Resource: c,
		Files:    nil,
		Registry: opts.Registry,
		Limits:   opts.Limits,
	}
	report, err := runArtifactAnalyze(ctx, subject, manifestDigest, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("analyze collection: %w", err)
	}

	findings := append([]analyze.Finding(nil), report.Findings...)
	rejected := report.Rejected
	for _, member := range memberResults {
		if member.Rejected {
			rejected = true
			findings = append(findings, member.Findings...)
		}
	}

	revisionDigest := ""
	if len(memberResults) > 0 {
		revManifest := RegistryRevisionManifest{
			SchemaVersion: RegistryRevisionSchemaV1,
			Generator:     RegistryRevisionGeneratorV1,
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
			})
		}
		_, digest, err := EncodeRegistryRevisionManifest(revManifest)
		if err != nil {
			return BuildResult{}, fmt.Errorf("encode revision manifest: %w", err)
		}
		revisionDigest = digest
	}

	rejectReason := ""
	decision := report.Decision
	if rejected {
		rejectReason = "policy rejected the collection or one of its members"
		decision = analyze.DecisionReject
	}
	return BuildResult{
		Artifact:        artifact,
		Name:            c.Name(),
		Kind:            "collection",
		License:         opts.Registry.License,
		Description:     description,
		ManifestPayload: payload,
		ManifestDigest:  manifestDigest,
		ContentDigest:   manifestDigest,
		RevisionDigest:  revisionDigest,
		Findings:        findings,
		Audits:          report.Audits,
		Decision:        decision,
		Capabilities:    caps,
		Rejected:        rejected,
		RejectReason:    rejectReason,
	}, nil
}

func buildRevisionDigest(r ir.Resource, contentDigest, archiveDigest string, _ analyze.Options) (string, error) {
	if r == nil {
		return "", nil
	}
	rev := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		Generator:     RegistryRevisionGeneratorV1,
		Resources: []RevisionResource{{
			Kind:          r.Kind().String(),
			Name:          r.Name(),
			ContentDigest: contentDigest,
			ArchiveDigest: archiveDigest,
		}},
	}
	_, digest, err := EncodeRegistryRevisionManifest(rev)
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
		if previous, ok := byKey[key]; ok && previous.ContentDigest != mr.ContentDigest {
			continue
		}
		byKey[key] = mr
	}

	resolved := make([]hccartifact.CollectionMemberEntry, 0, len(manifest.Members))
	for i := range manifest.Members {
		m := manifest.Members[i]
		key := m.Kind + "/" + m.Name
		if mr, ok := byKey[key]; ok {
			m.RegistryID = opts.Registry.ID
			m.Revision = mr.ContentDigest
		}
		resolved = append(resolved, m)
	}
	for _, mr := range memberResults {
		kind := mr.Kind
		if kind == "" {
			kind = memberKind(mr)
		}
		if kind != "collection" {
			continue
		}
		key := kind + "/" + mr.Name
		found := false
		for _, member := range resolved {
			if member.Kind+"/"+member.Name == key {
				found = true
				break
			}
		}
		if !found {
			resolved = append(resolved, hccartifact.CollectionMemberEntry{Kind: kind, Name: mr.Name, RegistryID: opts.Registry.ID, Revision: mr.ContentDigest})
		}
	}
	manifest.Members = resolved

	leaves := make([]hccartifact.CollectionMemberEntry, 0, len(memberResults))
	seen := make(map[string]int)
	for _, mr := range memberResults {
		kind := mr.Kind
		if kind == "" {
			kind = memberKind(mr)
		}
		if kind == "collection" {
			continue
		}
		key := kind + "/" + mr.Name + "/" + opts.Registry.ID
		leaf := hccartifact.CollectionMemberEntry{Kind: kind, Name: mr.Name, RegistryID: opts.Registry.ID, Revision: mr.ContentDigest, InclusionPaths: [][]string{{manifest.Name}}}
		if index, ok := seen[key]; ok {
			if leaves[index].Revision == leaf.Revision {
				leaves[index].InclusionPaths = append(leaves[index].InclusionPaths, leaf.InclusionPaths...)
			}
			continue
		}
		seen[key] = len(leaves)
		leaves = append(leaves, leaf)
	}
	manifest.Leaves = leaves
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
