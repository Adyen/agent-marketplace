package materialize

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// nativeHookProjection is the complete verified source-derived preparation of
// one selected hook for one (target, scope): the projected native artifacts,
// the portable support artifacts, the version-1 source digest, the authorized
// destination root, the documented native file layout, and the support
// namespace relative to that root. It carries no ownership state and writes
// nothing.
type nativeHookProjection struct {
	artifacts      []artifact.Artifact
	support        []artifact.Artifact
	sourceDigest   string
	outputRoot     string
	layout         nativeHookLayout
	supportRelBase string
}

// prepareNativeHookProjection captures the selected hook's verified source
// definition and support under the configured source root, proves the
// captured definition re-ingests to exactly the selected hook, and projects
// that captured hook through the shared catalog-driven native projection.
// Live IR is never projected without the captured-source agreement, so a
// stale selected resource cannot define the expected native entries.
func prepareNativeHookProjection(w skillTarget, repoRoot string) (nativeHookProjection, error) {
	res := w.skill
	hook, ok := res.Resource.(*ir.Hook)
	if !ok {
		return nativeHookProjection{}, errors.New("native hook projection requires a hook resource")
	}
	declared, ok := targetmeta.LookupHookProjection(targetmeta.Target(w.target))
	if !ok {
		return nativeHookProjection{}, fmt.Errorf("target %q declares no native hook projection", w.target)
	}
	file := declared.File(w.machineScope)
	destinations, err := harness.ResolveDestinations(w.target, w.machineScope, repoRoot)
	if err != nil {
		return nativeHookProjection{}, err
	}
	destination, layoutPath, err := nativeHookProjectionDestination(destinations, file)
	if err != nil {
		return nativeHookProjection{}, fmt.Errorf("target %q hook destination: %w", w.target, err)
	}
	supportDir, err := nativeHookProjectionSupportDir(declared.SupportDir, destination.Prefix)
	if err != nil {
		return nativeHookProjection{}, fmt.Errorf("target %q hook support destination: %w", w.target, err)
	}
	layout := nativeHookLayout{
		rel:       layoutPath,
		container: file.Member,
		shape:     declared.Shape,
		envelope:  file.Envelope,
	}
	if _, hasVersion := file.Envelope["version"]; hasVersion {
		layout.requireVersion = true
	}
	outputRoot := destination.Root

	var captured *ir.Hook
	var digest string
	var support []artifact.Artifact
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror {
		// Verified mirror acquisition carries no source tree: the decoded
		// resource and the verified intrinsic support inventory are the only
		// inputs, the mirror fingerprint domain supplies the source digest,
		// and the archive Path is never opened as a source root.
		if res.MarketplaceHookSupport == nil {
			return nativeHookProjection{}, errors.New("mirror hook support inventory is unknown")
		}
		digest, err = nativeHookFingerprint(res, w.target, w.machineScope)
		if err != nil {
			return nativeHookProjection{}, err
		}
		captured = hook
		support, err = marketplaceHookSupportArtifacts(res.MarketplaceHookSupport)
		if err != nil {
			return nativeHookProjection{}, err
		}
	} else {
		root, definition, definitionDir, err := nativeHookProjectionSource(res)
		if err != nil {
			return nativeHookProjection{}, err
		}
		projection := res
		sourceHarness, err := capturedHookSourceHarness(res, hook)
		if err != nil {
			return nativeHookProjection{}, err
		}
		projection.SourceHarness = string(sourceHarness)
		excluded, _ := nativeHookSourceExclusions(projection, definitionDir)
		definitionName := path.Base(definition)
		peers := make([]string, 0, len(excluded))
		for peer := range excluded {
			if peer != definitionName {
				peers = append(peers, peer)
			}
		}
		sort.Strings(peers)
		snapshot, err := captureHookSourceSnapshot(root, definition, definitionDir, peers...)
		if err != nil {
			return nativeHookProjection{}, err
		}
		captured, err = verifyCapturedSourceHook(res, hook, snapshot)
		if err != nil {
			return nativeHookProjection{}, err
		}
		input, err := nativeHookFingerprintInput(res, w.target, w.machineScope)
		if err != nil {
			return nativeHookProjection{}, err
		}
		digest, err = hookSourceFingerprintFromSnapshot(input, snapshot)
		if err != nil {
			return nativeHookProjection{}, err
		}
		support, err = hookSupportArtifactsFromSnapshot(projection, snapshot)
		if err != nil {
			return nativeHookProjection{}, err
		}
	}
	sourceKey, err := hookSupportSourceKey(res)
	if err != nil {
		return nativeHookProjection{}, err
	}
	name := res.InstalledName
	if name == "" {
		name = res.Name
	}
	if err := ir.ValidateName(name); err != nil {
		return nativeHookProjection{}, err
	}
	supportRelBase := path.Join(supportDir, sourceKey, name)
	supportBase, err := hookProjectionCommandBase(w, outputRoot, repoRoot, supportRelBase)
	if err != nil {
		return nativeHookProjection{}, err
	}
	scope := hcc.ScopeProject
	if w.machineScope {
		scope = hcc.ScopeUser
	}
	projected, err := hcc.CompileNativeHookProjection(captured, w.target, scope, supportBase)
	if err != nil {
		return nativeHookProjection{}, err
	}
	if projected.HasErrors() {
		return nativeHookProjection{}, fmt.Errorf("native hook projection refused the captured definition")
	}
	return nativeHookProjection{
		artifacts:      projected.Artifacts,
		support:        support,
		sourceDigest:   digest,
		outputRoot:     outputRoot,
		layout:         layout,
		supportRelBase: supportRelBase,
	}, nil
}

func nativeHookProjectionDestination(destinations []harness.Destination, file targetmeta.NativeHookProjectionFile) (harness.Destination, string, error) {
	var selected harness.Destination
	var selectedPath string
	found := false
	for _, destination := range destinations {
		relative, err := destination.RouteArtifact(file.Path)
		if err != nil {
			continue
		}
		if len(destination.AggregateRoutes) > 0 {
			matched := false
			for _, route := range destination.AggregateRoutes {
				if route.ArtifactPath != file.Path {
					continue
				}
				if route.Container != file.Member {
					matched = false
					break
				}
				matched = true
			}
			if !matched {
				continue
			}
		}
		if !found {
			selected = destination
			selectedPath = relative
			found = true
			continue
		}
		if filepath.Clean(selected.Root) != filepath.Clean(destination.Root) || selectedPath != relative || selected.Prefix != destination.Prefix {
			return harness.Destination{}, "", fmt.Errorf("artifact %q matches multiple destination routes", file.Path)
		}
	}
	if !found {
		return harness.Destination{}, "", fmt.Errorf("artifact %q has no authorized destination route", file.Path)
	}
	return selected, selectedPath, nil
}

func nativeHookProjectionSupportDir(supportDir, prefix string) (string, error) {
	supportDir = path.Clean(supportDir)
	prefix = path.Clean(prefix)
	if prefix == "." {
		prefix = ""
	}
	if prefix == "" {
		return supportDir, nil
	}
	if supportDir == prefix {
		return ".", nil
	}
	if strings.HasPrefix(supportDir, prefix+"/") {
		return strings.TrimPrefix(supportDir, prefix+"/"), nil
	}
	return "", fmt.Errorf("hook support directory %q is outside destination prefix %q", supportDir, prefix)
}

// hookProjectionCommandBase resolves the support base the projection embeds
// in hook commands: repository scope stays a clean path relative to the
// repository root so commands survive a moved checkout, while machine scope
// keeps the absolute authorized root.
func hookProjectionCommandBase(w skillTarget, outputRoot, repoRoot, supportRelBase string) (string, error) {
	abs := filepath.Join(outputRoot, filepath.FromSlash(supportRelBase))
	if w.machineScope {
		return abs, nil
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	repoRoots := []string{filepath.Clean(repoAbs)}
	if canonical, cerr := ownership.CanonicalRoot(repoRoot); cerr == nil && canonical != repoRoots[0] {
		repoRoots = append(repoRoots, canonical)
	}
	var repoRel string
	matched := false
	for _, root := range repoRoots {
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			continue
		}
		repoRel = filepath.ToSlash(rel)
		if repoRel != ".." && !strings.HasPrefix(repoRel, "../") && !path.IsAbs(repoRel) {
			matched = true
			break
		}
	}
	if !matched {
		return "", fmt.Errorf("hook support directory %s is outside the repository root", supportRelBase)
	}
	return repoRel, nil
}
