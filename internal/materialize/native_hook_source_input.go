package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	factoryingest "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var capturedNativeHookSourceLoaders = map[hcc.Harness]func(fs.FS, string) ([]*ir.Hook, []diag.Diagnostic, error){
	hcc.Harness(hcc.TargetFactory): factoryingest.LoadHooksForNativeProjection,
}

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
			return nativeHookProjectionSourceLocation(res.StagedSourceDir, res.Path)
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
	return nativeHookProjectionSourceLocation(root, definition)
}

func nativeHookProjectionSourceLocation(sourceRoot, definitionPath string) (root, definition, definitionDir string, err error) {
	root, definition, definitionDir, ok, err := hookSupportSourceLocation(sourceRoot, definitionPath)
	if err != nil {
		return "", "", "", err
	}
	if !ok {
		return "", "", "", errors.New("hook source location is unavailable")
	}
	return root, definition, definitionDir, nil
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
	nativeLoader, ok := capturedNativeHookSourceLoaders[sourceHarness]
	if !ok {
		return legacyHook, nil
	}

	nativeHooks, nativeDiagnostics, err := nativeLoader(snapshotFS, sourceFile)
	if err != nil {
		return nil, errors.New("captured native hook definition could not be re-ingested")
	}
	nativeBundle := &ingest.Bundle{Diagnostics: nativeDiagnostics, Resources: make([]ir.Resource, 0, len(nativeHooks))}
	for _, nativeHook := range nativeHooks {
		nativeBundle.Resources = append(nativeBundle.Resources, nativeHook)
	}
	return selectCapturedSourceHook(nativeBundle, hook.Name())
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
