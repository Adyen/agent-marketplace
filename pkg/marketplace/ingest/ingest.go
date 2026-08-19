// Package ingest builds Marketplace source revisions from cloned source trees.
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
		entry  catalog.Entry
		result artifact.BuildResult
	}

	var processedResults []processed
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

			r, err := buildResource(ctx, source, root, entry, origin, commit, dir, policy, limits, allowlist)
			if err != nil {
				return rev, fmt.Errorf("build %s %q: %w", kind, entry.Name, err)
			}
			r = applyPolicyToResult(r, limits, allowlist, source.Acquisition)
			processedResults = append(processedResults, processed{entry: entry, result: r})
		}
	}

	var manifest artifact.SourceRevisionManifest
	manifest.SchemaVersion = artifact.SourceRevisionSchemaV1
	manifest.SourceURL = origin
	manifest.Ref = source.Ref
	manifest.Generator = fmt.Sprintf("agent-marketplace source-revision v%s", analyze.AnalyzerVersionV1)
	manifest.Resources = make([]artifact.RevisionResource, 0, len(processedResults))

	for _, p := range processedResults {
		kind := api.FromIR(p.entry.Kind)
		manifest.Resources = append(manifest.Resources, artifact.RevisionResource{
			Kind:          kind,
			Name:          p.entry.Name,
			ContentDigest: p.result.ContentDigest,
			ArchiveDigest: p.result.ArchiveDigest,
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
		Occurrence:      occ,
		Resources:       resources,
		ExtraBlobs:      extraBlobs,
		ManifestPayload: payload,
		ManifestDigest:  store.RevisionDigest(digest),
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

func buildResource(ctx context.Context, source store.Source, root store.SourceRoot, entry catalog.Entry, origin, commit, dir string, policy analyze.Policy, limits config.Limits, allowlist []string) (artifact.BuildResult, error) {
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

	files, err := fileSetForResource(dir, entry)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("support files: %w", err)
	}

	switch r := entry.Resource.(type) {
	case *hccir.Collection:
		return artifact.BuildCollectionManifest(ctx, r, nil, opts)
	case hccir.Resource:
		return artifact.BuildResourceArtifact(ctx, r, files, opts)
	default:
		return artifact.BuildResult{}, fmt.Errorf("unknown resource type %T", entry.Resource)
	}
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

func supportFilesFor(r hccir.Resource) ([]string, error) {
	switch v := r.(type) {
	case *hccir.Skill:
		return v.SupportFiles, nil
	case *hccir.Command:
		return nil, nil
	case *hccir.Subagent:
		return nil, nil
	case *hccir.Rule:
		return nil, nil
	case *hccir.Hook:
		return nil, nil
	case *hccir.McpServer:
		return nil, nil
	case *hccir.Collection:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported resource type %T", r)
	}
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
	switch v := r.(type) {
	case *hccir.Skill:
		return v.Extra
	case *hccir.Command:
		return v.Extra
	case *hccir.Subagent:
		return v.Extra
	case *hccir.Rule:
		return v.Extra
	case *hccir.Hook:
		return v.Extra
	case *hccir.McpServer:
		return v.Extra
	case *hccir.Collection:
		return v.Extra
	default:
		return nil
	}
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

func applyPolicyToResult(r artifact.BuildResult, limits config.Limits, allowlist []string, mode store.AcquisitionMode) artifact.BuildResult {
	if mode == store.AcquisitionIndexOnly {
		r.RawArchive = nil
		r.ArchiveDigest = ""
	}
	if r.Rejected {
		r.RawArchive = nil
		r.ArchiveDigest = ""
		return r
	}

	if len(allowlist) > 0 && !contains(allowlist, r.License) {
		r.Rejected = true
		r.RawArchive = nil
		r.ArchiveDigest = ""
		r.Findings = append(r.Findings, analyze.Finding{
			Code:     "marketplace.policy.license-not-allowed",
			Severity: analyze.OutcomeReject,
			Message:  fmt.Sprintf("license %q is not in the configured license allowlist", r.License),
		})
		return r
	}

	if limits.ArtifactBytes > 0 && len(r.RawArchive) > int(limits.ArtifactBytes) {
		r.Rejected = true
		r.RawArchive = nil
		r.ArchiveDigest = ""
		r.Findings = append(r.Findings, analyze.Finding{
			Code:     "marketplace.policy.artifact-too-large",
			Severity: analyze.OutcomeReject,
			Message:  fmt.Sprintf("artifact size %d exceeds limit %d", len(r.RawArchive), limits.ArtifactBytes),
		})
		return r
	}

	if mode == store.AcquisitionIndexOnly {
		r.RawArchive = nil
		r.ArchiveDigest = ""
	}

	return r
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
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
