package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"slices"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	factoryingest "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// nativeHookProjectionSource resolves the verified source layout root and the
// hook definition's root-relative path for one selected non-mirror hook,
// using the same rules the version-1 fingerprint derives: local and Git
// sources under their configured roots and index-only Marketplace pins under
// their verified staged source directory. Mirror acquisition retains no
// source tree and is refused rather than reading the artifact archive.
func nativeHookProjectionSource(res resolver.ResolvedArtifact) (root, definition, definitionDir string, err error) {
	if res.IsMarketplace {
		switch res.MarketplaceAcquisition {
		case config.MarketplaceAcquisitionMirror:
			return "", "", "", errors.New("mirror hook acquisition retains no original source tree")
		case config.MarketplaceAcquisitionIndexOnly:
			if res.StagedSourceDir == "" || res.Path == "" ||
				!filepath.IsAbs(res.StagedSourceDir) || !filepath.IsAbs(res.Path) {
				return "", "", "", fmt.Errorf("index-only hook source is incomplete")
			}
			return hookSupportSourceLocation(res.StagedSourceDir, res.Path)
		default:
			return "", "", "", fmt.Errorf("marketplace hook source mode is unavailable")
		}
	}

	sourceFile := filepath.Clean(filepath.FromSlash(compilepkg.SourceOf(res.Resource).File))
	if !res.IsLocal() && filepath.IsAbs(sourceFile) && sourceFile != filepath.Clean(res.Path) {
		relative := commonTrailingSlashPath(sourceFile, filepath.Clean(res.Path))
		if relative == "" || !fs.ValidPath(relative) || relative == "." {
			return "", "", "", fmt.Errorf("pinned hook definition has no valid source-relative path")
		}
		root := sourceFile
		for range strings.Split(relative, "/") {
			root = filepath.Dir(root)
		}
		return filepath.Clean(root), relative, path.Dir(relative), nil
	}

	definition = materializationSourcePath(res)
	if definition == "" {
		return "", "", "", fmt.Errorf("hook definition path is unavailable")
	}
	root = ""
	if res.IsLocal() {
		root = res.RegistryURL
	} else if res.RegistryURL != "" && res.CatalogKey != "" {
		root = cache.BaseCachePath(res.CatalogKey)
	}
	if root == "" {
		return "", "", "", fmt.Errorf("hook definition has no configured source root")
	}
	if res.RegistryPath != "" {
		raw := filepath.ToSlash(filepath.FromSlash(res.RegistryPath))
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(res.RegistryPath)))
		if raw != rel || rel != "." && !fs.ValidPath(rel) {
			return "", "", "", fmt.Errorf("configured hook source path is invalid")
		}
		if rel != "." {
			root = filepath.Join(root, filepath.FromSlash(rel))
		}
	}
	return hookSupportSourceLocation(root, definition)
}

func hookSupportSourceLocation(sourceRoot, definitionPath string) (root, definition, definitionDir string, err error) {
	root, err = filepath.Abs(sourceRoot)
	if err != nil {
		return "", "", "", err
	}
	if filepath.IsAbs(definitionPath) {
		if filepath.Clean(definitionPath) != definitionPath {
			return "", "", "", fmt.Errorf("hook definition path is not clean")
		}
	} else {
		rel := filepath.ToSlash(filepath.FromSlash(definitionPath))
		if !fs.ValidPath(rel) {
			return "", "", "", fmt.Errorf("hook definition path is invalid")
		}
		definitionPath = filepath.Join(root, filepath.FromSlash(rel))
	}
	definitionPath, err = filepath.Abs(definitionPath)
	if err != nil {
		return "", "", "", err
	}
	rel, err := filepath.Rel(root, definitionPath)
	if err != nil {
		return "", "", "", err
	}
	definition = filepath.ToSlash(filepath.Clean(rel))
	if definition == "." || !fs.ValidPath(definition) {
		return "", "", "", fmt.Errorf("hook definition is outside its configured source root")
	}
	return filepath.Clean(root), definition, path.Dir(definition), nil
}

// hookSupportSourceKey derives the stable per-source namespace component the
// native writer installs support under: the configured-source slug plus the
// sha256 of the version-1 stable source identity, so two configured sources
// can never share a namespace.
func hookSupportSourceKey(res resolver.ResolvedArtifact) (string, error) {
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(identity))
	return hookSupportSlug(res) + "-" + hex.EncodeToString(sum[:]), nil
}

func hookSupportSlug(res resolver.ResolvedArtifact) string {
	raw := ""
	if res.IsMarketplace {
		raw = strings.TrimSpace(res.MarketplaceRegistryID)
		if name := strings.TrimSpace(res.MarketplaceResourceName); name != "" {
			if raw != "" {
				raw += "-" + name
			} else {
				raw = name
			}
		}
	} else {
		raw = strings.TrimSpace(res.RegistryURL)
	}
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	if i := strings.LastIndexAny(raw, "/\\"); i >= 0 {
		raw = raw[i+1:]
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			if lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
			continue
		}
		b.WriteRune(r)
		lastDash = r == '-'
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" {
		return "source"
	}
	if len(slug) > 64 {
		slug = strings.Trim(slug[:64], "-.")
	}
	if slug == "" {
		return "source"
	}
	return slug
}

// capturedHookSourceHarness reports the harness the captured definition is
// re-ingested under: the selected resource's recorded source harness first,
// then the hook's own native provenance, with Claude as the canonical-source
// default. A resource-recorded source harness contradicting the hook's
// explicit native provenance refuses rather than guessing which is right.
func capturedHookSourceHarness(res resolver.ResolvedArtifact, hook *ir.Hook) (hcc.Harness, error) {
	if res.SourceHarness != "" && hook.HookSource != "" && res.SourceHarness != hook.HookSource {
		return "", fmt.Errorf("hook source harness %q contradicts explicit hook provenance %q", res.SourceHarness, hook.HookSource)
	}
	source := res.SourceHarness
	if source == "" {
		source = hook.HookSource
	}
	if source == "" {
		source = string(hcc.TargetClaude)
	}
	if _, ok := ingest.For(ingest.Harness(source)); !ok {
		return "", errors.New("hook source harness is unavailable")
	}
	return hcc.Harness(source), nil
}

// verifyCapturedSourceHook proves the captured definition re-ingests to the
// selected IR through the legacy loader. Factory sources are then reparsed
// through the opt-in native loader on the same snapshot filesystem; other
// sources use the legacy result. Missing, duplicate, error-diagnostic or stale
// matches refuse; the native projection never consumes the live IR.
func verifyCapturedSourceHook(res resolver.ResolvedArtifact, hook *ir.Hook, snapshot *hookSourceSnapshot) (*ir.Hook, error) {
	if snapshot == nil || snapshot.definitionRel == "" {
		return nil, errors.New("hook source definition snapshot is unavailable")
	}
	sourceFile := filepath.ToSlash(filepath.Clean(filepath.FromSlash(hook.Source.File)))
	if filepath.IsAbs(sourceFile) {
		sourceFile = snapshot.definitionRel
	}
	if sourceFile == "" || sourceFile == "." || strings.Contains(sourceFile, "\\") || path.IsAbs(sourceFile) || !fs.ValidPath(sourceFile) || path.Clean(sourceFile) != sourceFile {
		return nil, errors.New("hook source definition path is invalid")
	}
	sourceHarness, err := capturedHookSourceHarness(res, hook)
	if err != nil {
		return nil, err
	}
	snapshotFS := hookDefinitionSnapshotFS{definition: sourceFile, data: snapshot.definitionData}
	legacyBundle, err := hcc.IngestBundle(snapshotFS, sourceHarness)
	if err != nil {
		return nil, errors.New("captured hook definition could not be re-ingested")
	}
	legacyHook, err := selectCapturedSourceHook(legacyBundle, hook.Name())
	if err != nil {
		return nil, err
	}
	want, err := canonicalHookPayload(hook)
	if err != nil {
		return nil, errors.New("selected hook payload could not be canonicalized")
	}
	got, err := canonicalHookPayload(legacyHook)
	if err != nil {
		return nil, errors.New("re-ingested hook payload could not be canonicalized")
	}
	if !bytes.Equal(want, got) {
		return nil, errors.New("captured hook definition differs from the selected resource")
	}
	if sourceHarness != hcc.Harness(hcc.TargetFactory) {
		return legacyHook, nil
	}

	nativeHooks, nativeDiagnostics, err := factoryingest.LoadHooksForNativeProjection(snapshotFS, sourceFile)
	if err != nil {
		return nil, errors.New("captured native Factory hook definition could not be re-ingested")
	}
	nativeBundle := &ingest.Bundle{Diagnostics: nativeDiagnostics, Resources: make([]ir.Resource, 0, len(nativeHooks))}
	for _, nativeHook := range nativeHooks {
		nativeBundle.Resources = append(nativeBundle.Resources, nativeHook)
	}
	return selectCapturedSourceHook(nativeBundle, hook.Name())
}

// marketplaceHookSupportArtifacts validates and immutably copies the verified
// mirror support inventory into projected support artifacts: each path must
// be a clean support-relative file path, duplicates refuse, and the copy is
// sorted so callers see a deterministic inventory.
func marketplaceHookSupportArtifacts(support []artifact.Artifact) ([]artifact.Artifact, error) {
	files := make([]artifact.Artifact, 0, len(support))
	seen := make(map[string]struct{}, len(support))
	for _, source := range support {
		file := artifact.Artifact{Path: source.Path, Bytes: bytes.Clone(source.Bytes), Mode: source.Mode.Perm()}
		if err := file.Validate(); err != nil {
			return nil, fmt.Errorf("%w: mirror hook support path is invalid: %v", fsboundary.ErrUnsafePath, err)
		}
		if _, ok := seen[file.Path]; ok {
			return nil, fmt.Errorf("%w: mirror hook support contains a duplicate path", fsboundary.ErrUnsafePath)
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	slices.SortFunc(files, func(a, b artifact.Artifact) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

func selectCapturedSourceHook(bundle *ingest.Bundle, name string) (*ir.Hook, error) {
	if bundle == nil {
		return nil, errors.New("hook ingestion returned no bundle")
	}
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Severity == diag.SeverityError {
			return nil, errors.New("hook ingestion reported an error")
		}
	}
	var match *ir.Hook
	for _, resource := range bundle.Resources {
		hook, ok := resource.(*ir.Hook)
		if !ok || hook.Name() != name {
			continue
		}
		if match != nil {
			return nil, errors.New("hook source name is ambiguous")
		}
		match = hook
	}
	if match == nil {
		return nil, errors.New("hook source name is missing")
	}
	return match, nil
}
