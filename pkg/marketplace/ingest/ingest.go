// Package ingest builds Marketplace source revisions from cloned source trees.
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	hccir "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Revision is the result of ingesting a single source commit.
type Revision struct {
	Occurrence      store.SourceOccurrence
	Resources       []store.ResourceRevision
	ExtraBlobs      []store.BlobUpload
	ManifestPayload []byte
	ManifestDigest  store.RevisionDigest
	// ResourceArchives retains locally-built canonical archives for index-only
	// consumers. They are never published by the worker.
	ResourceArchives      map[string][]byte
	ResourceArchiveDigest map[string]string
}

// BuildSourceRevision discovers resources under dir for the given source and
// commit, builds artifacts, applies policy, and produces a publishable revision.
func BuildSourceRevision(ctx context.Context, cfg config.Config, mpID store.MarketplaceID, source store.Source, dir, commit string) (Revision, error) {
	var rev Revision

	if mpID == "" {
		return rev, fmt.Errorf("marketplace id is required")
	}
	if source.ID == "" {
		return rev, fmt.Errorf("source id is required")
	}
	if dir == "" || commit == "" {
		return rev, fmt.Errorf("dir and commit are required")
	}
	if len(source.Origins) == 0 {
		return rev, fmt.Errorf("source %q has no origin", source.ID)
	}
	origin := source.Origins[len(source.Origins)-1]

	policy := effectivePolicy(cfg, source.PolicyOverrides)
	limits := effectiveLimits(cfg.Policy.Limits, source.PolicyOverrides)
	allowlist := effectiveLicenseAllowlist(cfg.Policy.LicenseAllowlist, source.PolicyOverrides)

	ctx, cancel := context.WithTimeout(ctx, limits.IngestionDuration.Duration())
	defer cancel()

	type processed struct {
		entry        catalog.Entry
		result       artifact.BuildResult
		supportFiles map[string]string
	}

	var processedResults []processed
	resourceResults := make(map[string]processed)
	resourceArchives := make(map[string][]byte)
	resourceArchiveDigests := make(map[string]string)
	var collectionEntries []catalog.Entry
	seen := make(map[string]bool)

	for _, root := range source.Roots {
		rootDir := filepath.Join(dir, filepath.Clean(root.Path))
		rel, err := filepath.Rel(dir, rootDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rev, fmt.Errorf("root %q escapes source directory", root.Path)
		}
		if _, err := os.Stat(rootDir); err != nil {
			if os.IsNotExist(err) {
				return rev, fmt.Errorf("root path %q does not exist", root.Path)
			}
			return rev, fmt.Errorf("stat root %q: %w", root.Path, err)
		}

		cat, err := catalog.Load(rootDir, root.SourceHarness)
		if err != nil {
			return rev, fmt.Errorf("load catalog at %q: %w", root.Path, err)
		}

		for _, d := range cat.Diagnostics {
			if d.IsError() {
				slog.Warn("catalog diagnostic", "path", root.Path, "code", d.Code, "message", d.Message)
			} else {
				slog.Info("catalog diagnostic", "path", root.Path, "code", d.Code, "message", d.Message)
			}
		}
		if diag.HasErrors(cat.Diagnostics) {
			d := firstErrorDiagnostic(cat.Diagnostics)
			return rev, fmt.Errorf("catalog error at %q: %s: %s", root.Path, d.Code, d.Message)
		}

		cols, err := collection.DiscoverCollections(rootDir, string(source.Slug), origin, source.Ref, root.Path)
		if err != nil {
			return rev, fmt.Errorf("discover collections at %q: %w", root.Path, err)
		}
		colEntries, err := catalog.EntriesFromCollections(cols)
		if err != nil {
			return rev, fmt.Errorf("collection entries at %q: %w", root.Path, err)
		}

		entries := append(cat.Entries, colEntries...)

		for _, entry := range entries {
			kind := api.FromIR(entry.Kind)
			if kind == "" {
				slog.Debug("skipping unsupported resource kind", "kind", entry.Kind, "name", entry.Name)
				continue
			}
			key := kind + "/" + entry.Name
			if seen[key] {
				return rev, fmt.Errorf("duplicate resource %q in source %q", key, source.ID)
			}
			seen[key] = true

			if entry.Kind == hccir.KindCollection {
				collectionEntries = append(collectionEntries, entry)
				continue
			}

			files, err := fileSetForResource(dir, entry)
			if err != nil {
				return rev, fmt.Errorf("support files for %s %q: %w", kind, entry.Name, err)
			}
			r, err := buildResource(ctx, source, root, entry, origin, commit, policy, limits, allowlist, files, nil)
			if err != nil {
				return rev, fmt.Errorf("build %s %q: %w", kind, entry.Name, err)
			}
			rawArchive, rawArchiveDigest := r.RawArchive, r.ArchiveDigest
			r = applyPolicyToResult(r, limits, allowlist, source.Acquisition)
			if source.Acquisition == store.AcquisitionIndexOnly && !r.Rejected && len(rawArchive) > 0 {
				resourceArchives[key] = rawArchive
				resourceArchiveDigests[key] = rawArchiveDigest
			}
			p := processed{entry: entry, result: r, supportFiles: supportFileDigests(files)}
			processedResults = append(processedResults, p)
			resourceResults[key] = p
		}
	}

	collectionByName := make(map[string]catalog.Entry, len(collectionEntries))
	for _, e := range collectionEntries {
		collectionByName[e.Name] = e
	}

	type resolvedCollection struct {
		result  artifact.BuildResult
		members []artifact.BuildResult
	}
	collectionCache := make(map[string]resolvedCollection)

	var resolveCollection func(name string, visiting map[string]bool) (artifact.BuildResult, []artifact.BuildResult, error)
	resolveCollection = func(name string, visiting map[string]bool) (artifact.BuildResult, []artifact.BuildResult, error) {
		if visiting[name] {
			return artifact.BuildResult{}, nil, fmt.Errorf("cyclic collection reference: %q", name)
		}
		if cached, ok := collectionCache[name]; ok {
			return cached.result, cached.members, nil
		}

		entry, ok := collectionByName[name]
		if !ok {
			return artifact.BuildResult{}, nil, fmt.Errorf("collection %q not found", name)
		}

		c, ok := entry.Resource.(*hccir.Collection)
		if !ok {
			return artifact.BuildResult{}, nil, fmt.Errorf("resource %q is not a collection", name)
		}

		visiting[name] = true
		defer func() { delete(visiting, name) }()

		root := store.SourceRoot{Path: c.SourcePath}

		var members []artifact.BuildResult
		for _, m := range c.Members {
			memberKind := m.Kind
			if memberKind == 0 {
				memberKind = hccir.KindSkill
			}
			effectiveRegistry := m.Registry
			if effectiveRegistry == "" {
				effectiveRegistry = c.SourceRegistryURL
			}
			effectiveRef := m.Ref
			if effectiveRef == "" {
				effectiveRef = c.SourceRef
			}
			if effectiveRegistry != origin || effectiveRef != source.Ref {
				return artifact.BuildResult{}, nil, fmt.Errorf("collection %q member %q references external registry/ref which is not supported", name, m.Name)
			}

			if memberKind == hccir.KindCollection {
				res, mems, err := resolveCollection(m.Name, visiting)
				if err != nil {
					return artifact.BuildResult{}, nil, err
				}
				members = append(members, res)
				members = append(members, mems...)
				continue
			}

			kind := api.FromIR(memberKind)
			key := kind + "/" + m.Name
			rp, ok := resourceResults[key]
			if !ok {
				return artifact.BuildResult{}, nil, fmt.Errorf("collection %q member %s %q not found in source", name, kind, m.Name)
			}
			members = append(members, rp.result)
		}

		for _, inc := range c.Includes {
			res, mems, err := resolveCollection(inc, visiting)
			if err != nil {
				return artifact.BuildResult{}, nil, err
			}
			_ = res
			members = append(members, mems...)
		}

		// Clear provenance-only source fields before analysis; the analyzer's
		// dangerous-url scanner would otherwise flag the file:// origin used for
		// local test fixtures. The origin is already captured in opts.Source.
		savedURL, savedRef, savedKey, savedPath := c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath
		c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath = "", "", "", ""
		r, err := buildResource(ctx, source, root, entry, origin, commit, policy, limits, allowlist, nil, members)
		c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath = savedURL, savedRef, savedKey, savedPath
		if err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("build collection %q: %w", name, err)
		}
		r = applyPolicyToResult(r, limits, allowlist, source.Acquisition)
		collectionCache[name] = resolvedCollection{result: r, members: members}
		return r, members, nil
	}

	for _, entry := range collectionEntries {
		r, _, err := resolveCollection(entry.Name, make(map[string]bool))
		if err != nil {
			return rev, fmt.Errorf("resolve collection %q: %w", entry.Name, err)
		}
		processedResults = append(processedResults, processed{entry: entry, result: r})
	}

	var manifest artifact.SourceRevisionManifest
	manifest.SchemaVersion = artifact.SourceRevisionSchemaV1
	manifest.Generator = fmt.Sprintf("agent-marketplace source-revision v%s", analyze.AnalyzerVersionV1)
	manifest.Resources = make([]artifact.RevisionResource, 0, len(processedResults))

	for _, p := range processedResults {
		kind := api.FromIR(p.entry.Kind)
		manifest.Resources = append(manifest.Resources, artifact.RevisionResource{
			Kind:          kind,
			Name:          p.entry.Name,
			ContentDigest: p.result.ContentDigest,
			ArchiveDigest: p.result.ArchiveDigest,
			SupportFiles:  p.supportFiles,
			Rejected:      p.result.Rejected,
		})
	}

	payload, digest, err := artifact.CanonicalSourceRevisionPayload(manifest)
	if err != nil {
		return rev, fmt.Errorf("canonical source revision manifest: %w", err)
	}

	occ := store.SourceOccurrence{
		SourceID:                     source.ID,
		Ref:                          source.Ref,
		Commit:                       commit,
		SourceRevisionManifestDigest: store.RevisionDigest(digest),
		Acquisition:                  source.Acquisition,
		OccurredAt:                   time.Now().UTC(),
	}

	var resources []store.ResourceRevision
	var extraBlobs []store.BlobUpload
	for _, p := range processedResults {
		kind := api.FromIR(p.entry.Kind)
		coord := store.ResourceCoordinate{
			Marketplace: mpID,
			Source:      source.ID,
			Kind:        kind,
			Name:        p.entry.Name,
		}

		rev := store.ResourceRevision{
			Coordinate:      coord,
			ContentDigest:   store.RevisionDigest(p.result.ContentDigest),
			ArchiveDigest:   store.ArchiveDigest(p.result.ArchiveDigest),
			AnalyzerVersion: analyze.AnalyzerVersionV1,
			Findings:        toStoreFindings(p.result.Findings),
			License:         p.result.License,
			Provenance: &store.Provenance{
				SourceID:       source.ID,
				Commit:         commit,
				ManifestDigest: store.RevisionDigest(digest),
			},
		}
		if p.result.Rejected {
			rev.ArchiveDigest = ""
			rev.ContentDigest = store.RevisionDigest(p.result.ContentDigest)
		}
		resources = append(resources, rev)

		if !p.result.Rejected && source.Acquisition == store.AcquisitionMirror && p.result.RawArchive != nil {
			extraBlobs = appendUpload(extraBlobs, store.ArchiveDigest(p.result.ArchiveDigest), p.result.RawArchive)
		}
	}

	return Revision{
		Occurrence:            occ,
		Resources:             resources,
		ExtraBlobs:            extraBlobs,
		ManifestPayload:       payload,
		ManifestDigest:        store.RevisionDigest(digest),
		ResourceArchives:      resourceArchives,
		ResourceArchiveDigest: resourceArchiveDigests,
	}, nil
}

func firstErrorDiagnostic(ds []diag.Diagnostic) diag.Diagnostic {
	for _, d := range ds {
		if d.IsError() {
			return d
		}
	}
	return diag.Diagnostic{}
}

func buildResource(ctx context.Context, source store.Source, root store.SourceRoot, entry catalog.Entry, origin, commit string, policy analyze.Policy, limits config.Limits, allowlist []string, files analyze.FileSet, memberResults []artifact.BuildResult) (artifact.BuildResult, error) {
	opts := analyze.DefaultOptions()
	opts.Policy = policy
	opts.Source = analyze.Source{
		RegistryURL: origin,
		Ref:         source.Ref,
		Path:        root.Path,
		ID:          string(source.ID),
		Slug:        source.Slug,
		License:     resourceLicense(entry.Resource, source.License),
	}
	opts.Limits = analyze.Limits{
		MaxFileBytes:  limits.FileBytes,
		MaxTotalBytes: limits.RepositoryBytes,
		MaxFiles:      limits.FileCount,
		MaxFindings:   defaultMaxFindings,
	}

	switch r := entry.Resource.(type) {
	case *hccir.Collection:
		if r.SourcePath != "" {
			opts.Source.Path = r.SourcePath
		}
		return artifact.BuildCollectionManifest(ctx, r, memberResults, opts)
	case hccir.Resource:
		return artifact.BuildResourceArtifact(ctx, r, files, opts)
	default:
		return artifact.BuildResult{}, fmt.Errorf("unknown resource type %T", entry.Resource)
	}
}

func supportFileDigests(files analyze.FileSet) map[string]string {
	if len(files) == 0 {
		return nil
	}
	digests := make(map[string]string, len(files))
	for name, data := range files {
		digests[name] = artifact.RevisionDigest(data)
	}
	return digests
}

func fileSetForResource(dir string, entry catalog.Entry) (analyze.FileSet, error) {
	files := make(analyze.FileSet)
	if entry.Path == "" {
		return files, nil
	}

	resFile := entry.Path
	if !filepath.IsAbs(resFile) {
		resFile = filepath.Join(dir, filepath.Clean(resFile))
	}
	resRoot := filepath.Dir(resFile)
	relRoot, err := filepath.Rel(dir, resRoot)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("resource source escapes source directory: %q", entry.Path)
	}

	supportFiles, err := supportFilesFor(entry.Resource)
	if err != nil {
		return nil, err
	}
	for _, rel := range supportFiles {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("support file path traversal: %q", rel)
		}
		path := filepath.Join(resRoot, filepath.Clean(rel))
		data, err := readBoundedFile(path, 2*1024*1024)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read support file %q: %w", rel, err)
		}
		files[rel] = data
	}
	return files, nil
}

func resourceInfo(r hccir.Resource) (extra map[string]any, supportFiles []string, err error) {
	switch v := r.(type) {
	case *hccir.Skill:
		return v.Extra, v.SupportFiles, nil
	case *hccir.Command:
		return v.Extra, nil, nil
	case *hccir.Subagent:
		return v.Extra, nil, nil
	case *hccir.Rule:
		return v.Extra, nil, nil
	case *hccir.Hook:
		return v.Extra, nil, nil
	case *hccir.McpServer:
		return v.Extra, nil, nil
	case *hccir.Collection:
		return v.Extra, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported resource type %T", r)
	}
}

func supportFilesFor(r hccir.Resource) ([]string, error) {
	_, supportFiles, err := resourceInfo(r)
	return supportFiles, err
}

func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file %q size %d exceeds %d", path, info.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

func resourceLicense(r hccir.Resource, fallback string) string {
	extra := resourceExtra(r)
	if extra == nil {
		return fallback
	}
	for _, key := range []string{"license", "License"} {
		if v, ok := extra[key].(string); ok && v != "" {
			return v
		}
	}
	if meta, ok := extra["metadata"].(map[string]any); ok {
		for _, key := range []string{"license", "License"} {
			if v, ok := meta[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func resourceExtra(r hccir.Resource) map[string]any {
	extra, _, err := resourceInfo(r)
	if err != nil {
		return nil
	}
	return extra
}

func effectivePolicy(cfg config.Config, overrides map[string]any) analyze.Policy {
	hidden := cfg.Policy.HiddenContent
	if overrides != nil {
		if v, ok := overrides["hidden_content"].(string); ok && (v == "reject" || v == "warn") {
			hidden = v
		}
	}
	if hidden == "warn" {
		return analyze.PrivatePolicy()
	}
	return analyze.PublicPolicy()
}

func effectiveLimits(base config.Limits, overrides map[string]any) config.Limits {
	out := base
	if overrides == nil {
		return out
	}
	l, ok := overrides["limits"].(map[string]any)
	if !ok {
		return out
	}
	if v, ok := toInt64(l["repository_bytes"]); ok {
		out.RepositoryBytes = v
	}
	if v, ok := toInt(l["file_count"]); ok {
		out.FileCount = v
	}
	if v, ok := toInt64(l["file_bytes"]); ok {
		out.FileBytes = v
	}
	if v, ok := toInt64(l["artifact_bytes"]); ok {
		out.ArtifactBytes = v
	}
	if v, ok := parseDuration(l["clone_duration"]); ok {
		out.CloneDuration = config.Duration(v)
	}
	if v, ok := parseDuration(l["ingestion_duration"]); ok {
		out.IngestionDuration = config.Duration(v)
	}
	return out
}

func effectiveLicenseAllowlist(base []string, overrides map[string]any) []string {
	if overrides == nil {
		return base
	}
	if v, ok := overrides["license_allowlist"].([]any); ok {
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return base
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func parseDuration(v any) (time.Duration, bool) {
	switch d := v.(type) {
	case time.Duration:
		return d, true
	case string:
		pd, err := time.ParseDuration(d)
		if err != nil {
			return 0, false
		}
		return pd, true
	}
	return 0, false
}

const defaultMaxFindings = 1000

func stripArchive(r *artifact.BuildResult) {
	r.RawArchive = nil
	r.ArchiveDigest = ""
}

func applyPolicyToResult(r artifact.BuildResult, limits config.Limits, allowlist []string, mode store.AcquisitionMode) artifact.BuildResult {
	if r.Rejected {
		stripArchive(&r)
		return r
	}

	if len(allowlist) > 0 && !slices.Contains(allowlist, r.License) {
		r.Rejected = true
		stripArchive(&r)
		r.Findings = append(r.Findings, analyze.Finding{
			Code:     "marketplace.policy.license-not-allowed",
			Severity: analyze.OutcomeReject,
			Message:  fmt.Sprintf("license %q is not in the configured license allowlist", r.License),
		})
		return r
	}

	if limits.ArtifactBytes > 0 && len(r.RawArchive) > int(limits.ArtifactBytes) {
		artifactSize := len(r.RawArchive)
		r.Rejected = true
		stripArchive(&r)
		r.Findings = append(r.Findings, analyze.Finding{
			Code:     "marketplace.policy.artifact-too-large",
			Severity: analyze.OutcomeReject,
			Message:  fmt.Sprintf("artifact size %d exceeds limit %d", artifactSize, limits.ArtifactBytes),
		})
		return r
	}

	if mode == store.AcquisitionIndexOnly {
		stripArchive(&r)
	}

	return r
}

func toStoreFindings(findings []analyze.Finding) []store.Finding {
	out := make([]store.Finding, 0, len(findings))
	for _, f := range findings {
		var sr string
		if f.SourceRange != nil {
			sr = f.SourceRange.File
			if f.SourceRange.Line > 0 {
				sr = fmt.Sprintf("%s:%d", sr, f.SourceRange.Line)
				if f.SourceRange.Column > 0 {
					sr = fmt.Sprintf("%s:%d", sr, f.SourceRange.Column)
				}
			}
		}
		out = append(out, store.Finding{
			Code:        f.Code,
			Severity:    f.Severity,
			Policy:      f.PolicyOutcome,
			Message:     f.Message,
			SourceRange: sr,
		})
	}
	return out
}

func appendUpload(existing []store.BlobUpload, digest store.ArchiveDigest, data []byte) []store.BlobUpload {
	for _, b := range existing {
		if b.Digest == digest {
			return existing
		}
	}
	return append(existing, store.BlobUpload{Digest: digest, Data: data})
}
