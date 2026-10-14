package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type hookSupportPlan struct {
	relBase                 string
	artifacts               []artifact.Artifact
	sourceDigest            string
	sourceSnapshot          *hookSourceSnapshot
	sourceExcludedPeers     []string
	err                     error
	supportAdmissionRefused bool
}

type nativeHookInstalledSupport struct {
	outputRoot            string
	relativePath          string
	outputPath            string
	cacheRoot             string
	expectedSymlinkTarget string
	mode                  config.MaterializationMode
	capturedBytes         []byte
	capturedMode          fs.FileMode
}

type hookSupportClaim struct {
	artifact  artifact.Artifact
	producers []int
	conflict  bool
}

func prepareHookSupport(work []skillTarget, repoRoots ...string) (map[int]hookSupportPlan, map[int][]string) {
	var repoRoot string
	if len(repoRoots) > 0 {
		repoRoot = repoRoots[0]
	}
	plans := make(map[int]hookSupportPlan)
	type captureKey struct {
		sourceKey, root, definition, definitionDir, harness string
	}
	snapshots := make(map[captureKey]*hookSourceSnapshot)
	snapshotErrs := make(map[captureKey]error)
	snapshotExcludedPeers := make(map[captureKey][]string)
	claims := make(map[string]*hookSupportClaim)
	var keys []string

	for i, w := range work {
		if w.skill.Resource == nil || w.skill.Resource.Kind() != ir.KindHook {
			continue
		}
		plan := hookSupportPlan{}
		var files []artifact.Artifact
		if w.skill.IsMarketplace && w.skill.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror && w.skill.MarketplaceHookSupport != nil {
			sourceKey, err := hookSupportSourceKeyForScope(w, repoRoot)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			plan.relBase, err = hookSupportRelBase(w, sourceKey)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			files, err = marketplaceHookSupportArtifacts(w.skill.MarketplaceHookSupport)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			plan.sourceDigest, plan.err = nativeHookFingerprint(w.skill, w.target, w.machineScope)
			if plan.err != nil {
				plans[i] = plan
				continue
			}
		} else {
			root, definition, definitionDir, ok, err := hookSupportSource(w.skill)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			if !ok {
				if hook, ok := w.skill.Resource.(*ir.Hook); ok && hook.SourceDefinitionDigest() != "" {
					plan.err = errors.New("hook source definition is unavailable")
				}
				plans[i] = plan
				continue
			}
			sourceKey, err := hookSupportSourceKeyForScope(w, repoRoot)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			plan.relBase, err = hookSupportRelBase(w, sourceKey)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			projection, excludedPeers, err := hookSupportProjection(w.skill, definition, definitionDir)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			key := captureKey{sourceKey: sourceKey, root: root, definition: definition, definitionDir: definitionDir, harness: projection.SourceHarness}
			snapshot, exists := snapshots[key]
			if !exists {
				snapshot, err = captureHookSourceSnapshot(root, definition, definitionDir, excludedPeers...)
				snapshots[key] = snapshot
				snapshotErrs[key] = err
				snapshotExcludedPeers[key] = append([]string(nil), excludedPeers...)
			}
			if err := snapshotErrs[key]; err != nil {
				plan.err = err
				plan.supportAdmissionRefused = true
				plans[i] = plan
				continue
			}
			plan.sourceSnapshot = snapshot
			plan.sourceExcludedPeers = append([]string(nil), snapshotExcludedPeers[key]...)
			files, err = hookSupportArtifactsFromSnapshot(projection, snapshot)
			if err != nil {
				plan.err = err
				plans[i] = plan
				continue
			}
			plan.sourceDigest, plan.err = preparedHookSourceDigest(w.skill, w.target, w.machineScope, snapshot)
			if plan.err != nil {
				plans[i] = plan
				continue
			}
		}
		for _, file := range files {
			staged := artifact.Artifact{Path: path.Join(plan.relBase, file.Path), Bytes: bytes.Clone(file.Bytes), Mode: file.Mode.Perm()}
			if err := staged.Validate(); err != nil {
				plan.err = err
				plan.artifacts = nil
				break
			}
			plan.artifacts = append(plan.artifacts, staged)
		}
		if plan.err != nil {
			plans[i] = plan
			continue
		}
		for _, staged := range plan.artifacts {
			claimKey := fmt.Sprintf("%s\x00%t\x00%s", w.target, w.machineScope, staged.Path)
			claim := claims[claimKey]
			if claim == nil {
				claim = &hookSupportClaim{artifact: staged}
				claims[claimKey] = claim
				keys = append(keys, claimKey)
			}
			if !bytes.Equal(claim.artifact.Bytes, staged.Bytes) || claim.artifact.Mode.Perm() != staged.Mode.Perm() {
				claim.conflict = true
			}
			claim.producers = append(claim.producers, i)
		}
		plans[i] = plan
	}
	conflicts := make(map[int][]string)
	for _, key := range keys {
		claim := claims[key]
		if !claim.conflict {
			continue
		}
		for _, i := range claim.producers {
			conflicts[i] = append(conflicts[i], claim.artifact.Path)
		}
	}
	return plans, conflicts
}

func hookSupportDirFor(w skillTarget) (string, error) {
	dir, _, err := hookSupportAggregateLayout(w)
	return dir, err
}

func hookSupportAggregateLayout(w skillTarget) (dir, aggregatePath string, err error) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(w.target), w.machineScope)
	if err != nil {
		return "", "", err
	}
	return path.Join(declaration.partition.Prefix, declaration.aggregate.SupportDir),
		path.Join(declaration.partition.Prefix, declaration.file.Path), nil
}

func hookSupportRelBase(w skillTarget, sourceKey string) (string, error) {
	dir, err := hookSupportDirFor(w)
	if err != nil {
		return "", err
	}
	name := w.skill.InstalledName
	if name == "" {
		name = w.skill.Name
	}
	if err := ir.ValidateName(name); err != nil {
		return "", err
	}
	return path.Join(dir, sourceKey, name), nil
}

func hookSupportSourceKey(res resolver.ResolvedArtifact) (string, error) {
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		return "", err
	}
	return hookSupportSourceKeyFromIdentity(res, identity), nil
}

func hookSupportSourceKeyFromIdentity(res resolver.ResolvedArtifact, identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hookSupportSlug(res) + "-" + hex.EncodeToString(sum[:])
}

func hookSupportSourceKeyForScope(w skillTarget, repoRoot string) (string, error) {
	if w.machineScope || repoRoot == "" || !w.skill.IsLocal() {
		return hookSupportSourceKey(w.skill)
	}
	rel, ok, err := repositoryLocalSourceRel(w.skill, repoRoot)
	if err != nil {
		return "", err
	}
	if !ok {
		return hookSupportSourceKey(w.skill)
	}
	return hookSupportSourceKeyFromIdentity(w.skill, "repository-local:"+rel), nil
}

func repositoryLocalSourceRel(res resolver.ResolvedArtifact, repoRoot string) (string, bool, error) {
	source, err := localRegistryRoot(res)
	if err != nil {
		return "", false, err
	}
	if res.RegistryPath != "" {
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(res.RegistryPath)))
		if rel != "." && !fs.ValidPath(rel) {
			return "", false, nil
		}
		source = filepath.Join(source, filepath.FromSlash(rel))
	}
	canonicalRepo, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		return "", false, err
	}
	canonicalSource, err := ownership.CanonicalRoot(source)
	if err != nil {
		return "", false, err
	}
	rel, err := filepath.Rel(canonicalRepo, canonicalSource)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, nil
	}
	return filepath.ToSlash(rel), true, nil
}

func hookSupportSlugRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
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
		if !hookSupportSlugRune(r) {
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

func hookSupportCommandBase(w skillTarget, repoRoot, relBase string) (string, error) {
	if relBase == "" {
		return "", nil
	}
	_, aggregatePath, err := hookSupportAggregateLayout(w)
	if err != nil {
		return "", err
	}
	destinations, err := harness.ResolveDestinations(w.target, w.machineScope, repoRoot)
	if err != nil {
		return "", err
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			continue
		}
		if _, err := destination.RouteArtifact(aggregatePath); err != nil {
			continue
		}
		rel, err := destination.RouteArtifact(relBase)
		if err != nil {
			continue
		}
		abs := filepath.Join(destination.Root, filepath.FromSlash(rel))
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
		matched := false
		var repoRel string
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
			return "", fmt.Errorf("hook support directory %s is outside the repository root", relBase)
		}
		return repoRel, nil
	}
	return "", fmt.Errorf("target %s has no hook support destination in this scope", w.target)
}

func localRegistryRoot(res resolver.ResolvedArtifact) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := config.RegistryEntry{URL: res.RegistryURL, Ref: res.Ref, V4Kind: res.V4Kind}.ResolveLocalPath(cwd)
	if err != nil {
		return "", err
	}
	return filepath.Abs(root)
}

func hookSupportSource(res resolver.ResolvedArtifact) (root, definition, definitionDir string, ok bool, err error) {
	if res.IsMarketplace {
		switch res.MarketplaceAcquisition {
		case config.MarketplaceAcquisitionMirror:
			p := res.Path
			if p == "" {
				return "", "", "", false, nil
			}
			if !filepath.IsAbs(p) {
				return "", "", "", false, fmt.Errorf("mirror support path is not absolute")
			}
			info, err := os.Lstat(p)
			if err != nil {
				return "", "", "", false, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", "", "", false, fmt.Errorf("mirror support path is a symlink")
			}
			if info.IsDir() {
				root, err := filepath.Abs(p)
				return root, "", "", true, err
			}
			if info.Mode().IsRegular() {
				return "", "", "", false, nil
			}
			return "", "", "", false, fmt.Errorf("mirror support path is not a regular archive or directory")
		case config.MarketplaceAcquisitionIndexOnly:
			if res.StagedSourceDir == "" || res.Path == "" ||
				!filepath.IsAbs(res.StagedSourceDir) || !filepath.IsAbs(res.Path) {
				return "", "", "", false, fmt.Errorf("index-only hook source is incomplete")
			}
			return hookSupportSourceLocation(res.StagedSourceDir, res.Path)
		default:
			return "", "", "", false, fmt.Errorf("marketplace hook source mode is unavailable")
		}
	}

	sourceFile := filepath.Clean(filepath.FromSlash(compilepkg.SourceOf(res.Resource).File))
	if !res.IsLocal() && filepath.IsAbs(sourceFile) && sourceFile != filepath.Clean(res.Path) {
		relative := commonTrailingSlashPath(sourceFile, filepath.Clean(res.Path))
		if relative == "" || !fs.ValidPath(relative) || relative == "." {
			return "", "", "", false, fmt.Errorf("pinned hook definition has no valid source-relative path")
		}
		root := sourceFile
		for range strings.Split(relative, "/") {
			root = filepath.Dir(root)
		}
		return filepath.Clean(root), relative, path.Dir(relative), true, nil
	}

	definition = materializationSourcePath(res)
	if definition == "" {
		return "", "", "", false, nil
	}
	root = ""
	if res.IsLocal() {
		local, err := localRegistryRoot(res)
		if err != nil {
			return "", "", "", false, err
		}
		root = local
	} else if res.RegistryURL != "" {
		if res.CatalogKey == "" {
			return "", "", "", false, fmt.Errorf("configured hook source has no cache root")
		}
		root = cache.BaseCachePath(res.CatalogKey)
	}
	if root == "" {
		return "", "", "", false, fmt.Errorf("hook definition has no configured source root")
	}
	if res.RegistryPath != "" {
		raw := filepath.ToSlash(filepath.FromSlash(res.RegistryPath))
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(res.RegistryPath)))
		if raw != rel || rel != "." && !fs.ValidPath(rel) {
			return "", "", "", false, fmt.Errorf("configured hook source path is invalid")
		}
		if rel != "." {
			root = filepath.Join(root, filepath.FromSlash(rel))
		}
	}
	return hookSupportSourceLocation(root, definition)
}

func hookSupportSourceLocation(sourceRoot, definitionPath string) (root, definition, definitionDir string, ok bool, err error) {
	root, err = filepath.Abs(sourceRoot)
	if err != nil {
		return "", "", "", false, err
	}
	if filepath.IsAbs(definitionPath) {
		if filepath.Clean(definitionPath) != definitionPath {
			return "", "", "", false, fmt.Errorf("hook definition path is not clean")
		}
	} else {
		rel := filepath.ToSlash(filepath.FromSlash(definitionPath))
		if !fs.ValidPath(rel) {
			return "", "", "", false, fmt.Errorf("hook definition path is invalid")
		}
		definitionPath = filepath.Join(root, filepath.FromSlash(rel))
	}
	definitionPath, err = filepath.Abs(definitionPath)
	if err != nil {
		return "", "", "", false, err
	}
	rel, err := filepath.Rel(root, definitionPath)
	if err != nil {
		return "", "", "", false, err
	}
	definition = filepath.ToSlash(filepath.Clean(rel))
	if definition == "." || !fs.ValidPath(definition) {
		return "", "", "", false, fmt.Errorf("hook definition is outside its configured source root")
	}
	return filepath.Clean(root), definition, path.Dir(definition), true, nil
}

func openHookSupportDirAt(rootFD int, relative string) (int, error) {
	if relative == "" || relative == "." {
		return unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 0)
	}
	if !fs.ValidPath(relative) {
		return -1, fmt.Errorf("hook support directory path is invalid")
	}
	currentFD := rootFD
	ownedFD := -1
	for _, component := range strings.Split(relative, "/") {
		nextFD, err := openFingerprintDir(currentFD, component)
		if ownedFD >= 0 {
			_ = unix.Close(ownedFD)
		}
		if err != nil {
			return -1, err
		}
		currentFD = nextFD
		ownedFD = nextFD
	}
	return ownedFD, nil
}

func openHookSupportFileAt(dirFD int, relative string, total int64) (*os.File, fs.FileMode, int64, error) {
	return openHookSupportFileAtWithOpener(dirFD, relative, total, openFingerprintFileAt)
}

func openHookSupportFileAtNoAliases(dirFD int, relative string, total int64) (*os.File, fs.FileMode, int64, error) {
	return openHookSupportFileAtWithOpener(dirFD, relative, total, openFingerprintFileAtNoAliases)
}

func openHookSupportFileAtWithOpener(dirFD int, relative string, total int64, openFile func(int, string) (*os.File, fs.FileMode, int64, error)) (*os.File, fs.FileMode, int64, error) {
	if !fs.ValidPath(relative) {
		return nil, 0, 0, fmt.Errorf("hook support file path is invalid")
	}
	file, mode, size, err := openFile(dirFD, relative)
	if err != nil {
		return nil, 0, 0, err
	}
	remaining := int64(hookSourceFingerprintMaxBytes) - total
	if size < 0 || remaining < 0 || size > remaining {
		_ = file.Close()
		return nil, 0, 0, fmt.Errorf("%w: hook support source exceeds the existing size limit", fsboundary.ErrTooLarge)
	}
	return file, mode.Perm(), size, nil
}

func verifyNativeHookSupportOutput(output nativeHookInstalledSupport) error {
	readRoot := output.outputRoot
	var beforeTarget string
	var beforeGeneration dependencytxn.Generation
	if output.mode == config.MaterializationModeSymlink {
		if output.expectedSymlinkTarget != filepath.Join(output.cacheRoot, filepath.FromSlash(output.relativePath)) {
			return fmt.Errorf("captured hook support symlink target is inconsistent")
		}
		var err error
		beforeTarget, beforeGeneration, err = dependencytxn.ReadSymlinkSnapshot(output.outputPath)
		if err != nil {
			return fmt.Errorf("snapshot installed hook support symlink: %w", err)
		}
		if beforeTarget != output.expectedSymlinkTarget {
			return fmt.Errorf("%w: installed hook support symlink target changed: %s", dependencytxn.ErrGenerationConflict, output.outputPath)
		}
		readRoot = output.cacheRoot
	} else if output.mode != config.MaterializationModeFile {
		return fmt.Errorf("unsupported hook support materialization mode")
	}

	rootFD, err := unix.Open(readRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open authorized hook support root: %w", err)
	}
	var total int64
	data, mode, readErr := readHookSourceSnapshotFile(rootFD, output.relativePath, &total)
	closeErr := unix.Close(rootFD)
	if readErr != nil {
		if closeErr != nil {
			readErr = errors.Join(readErr, closeErr)
		}
		return fmt.Errorf("read installed hook support: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close authorized hook support root: %w", closeErr)
	}
	if !bytes.Equal(data, output.capturedBytes) {
		return fmt.Errorf("installed hook support bytes changed: %s", output.outputPath)
	}
	if mode.Perm() != output.capturedMode.Perm() {
		return fmt.Errorf("installed hook support permissions changed: %s", output.outputPath)
	}
	if output.mode == config.MaterializationModeSymlink {
		afterTarget, afterGeneration, err := dependencytxn.ReadSymlinkSnapshot(output.outputPath)
		if err != nil {
			return fmt.Errorf("recheck installed hook support symlink: %w", err)
		}
		if afterTarget != beforeTarget || afterTarget != output.expectedSymlinkTarget || afterGeneration != beforeGeneration {
			return fmt.Errorf("%w: installed hook support symlink changed: %s", dependencytxn.ErrGenerationConflict, output.outputPath)
		}
	}
	return nil
}

func hookSupportProjection(res resolver.ResolvedArtifact, definition, definitionDir string) (resolver.ResolvedArtifact, []string, error) {
	projection := res
	exclusionDir := definitionDir
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionIndexOnly {
		hook, ok := res.Resource.(*ir.Hook)
		if !ok {
			return resolver.ResolvedArtifact{}, nil, errors.New("index-only source resource is not a hook")
		}
		sourceHarness, err := indexOnlyHookSourceHarness(res, hook)
		if err != nil {
			return resolver.ResolvedArtifact{}, nil, err
		}
		sourceFile := filepath.ToSlash(filepath.Clean(filepath.FromSlash(hook.Source.File)))
		if sourceFile == "." || filepath.IsAbs(sourceFile) || sourceFile == ".." || strings.HasPrefix(sourceFile, "../") || !fs.ValidPath(sourceFile) {
			return resolver.ResolvedArtifact{}, nil, fmt.Errorf("index-only hook source definition path is invalid")
		}
		projection.SourceHarness = string(sourceHarness)
		exclusionDir = path.Dir(sourceFile)
	}
	excluded, _ := hookSupportExclusions(projection, exclusionDir)
	definitionName := ""
	if definition != "" {
		definitionName = path.Base(definition)
	}
	peers := make([]string, 0, len(excluded))
	for peer := range excluded {
		if peer != definitionName {
			peers = append(peers, peer)
		}
	}
	slices.Sort(peers)
	return projection, peers, nil
}

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

func hookSupportExclusions(res resolver.ResolvedArtifact, definitionDir string) (map[string]bool, bool) {
	excluded := make(map[string]bool)
	if res.SourceHarness == "" {
		return excluded, false
	}
	if res.SourceHarness == "windsurf" && (definitionDir == ".windsurf/hooks" || definitionDir == "hooks") {
		return excluded, true
	}
	lookupDir := definitionDir
	sourceFile := filepath.Clean(filepath.FromSlash(compilepkg.SourceOf(res.Resource).File))
	rawRegistryPath := filepath.ToSlash(filepath.FromSlash(res.RegistryPath))
	registryPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(res.RegistryPath)))
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionIndexOnly {
		sourceFileRel := filepath.ToSlash(sourceFile)
		if sourceFileRel != "." && !filepath.IsAbs(sourceFile) && sourceFile != ".." &&
			!strings.HasPrefix(sourceFileRel, "../") && fs.ValidPath(sourceFileRel) {
			lookupDir = path.Dir(sourceFileRel)
		}
	} else if !res.IsLocal() && filepath.IsAbs(sourceFile) && sourceFile != filepath.Clean(res.Path) &&
		rawRegistryPath == registryPath && registryPath != "." && fs.ValidPath(registryPath) {
		if lookupDir == registryPath {
			lookupDir = "."
		} else if strings.HasPrefix(lookupDir, registryPath+"/") {
			lookupDir = strings.TrimPrefix(lookupDir, registryPath+"/")
		}
	}
	for _, aggregatePath := range hookSourceAggregatePaths(res.SourceHarness) {
		dir, name := path.Split(aggregatePath)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = "."
		}
		if dir == lookupDir {
			excluded[name] = true
		}
	}
	return excluded, false
}

// hookSourceAggregatePaths returns the source-harness native aggregate
// locations — project, user, and declared legacy — relative to the source
// root, resolved through the source target's registered native layout.
func hookSourceAggregatePaths(sourceHarness string) []string {
	definition, ok := targetmeta.Lookup(sourceHarness)
	if !ok {
		return nil
	}
	layout, ok := targetmeta.LookupNativeLayout(definition.Target)
	if !ok {
		return nil
	}
	var paths []string
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			for _, file := range []targetmeta.NativeAggregateFile{aggregate.Project, aggregate.User} {
				p := path.Join(definition.RepoRoot, partition.Prefix, file.Path)
				paths = append(paths, p)
				for _, legacy := range file.Legacy {
					paths = append(paths, path.Join(definition.RepoRoot, partition.Prefix, legacy))
				}
			}
		}
	}
	return paths
}

func retainBlockedHookArtifacts(outDir string, artifacts []artifact.Artifact) ([]artifact.Artifact, error) {
	result := slices.Clone(artifacts)
	for i, a := range result {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		var desired map[string]json.RawMessage
		if err := json.Unmarshal(a.Bytes, &desired); err != nil {
			return nil, err
		}
		if len(desired) == 0 {
			continue
		}
		_, nested := desired["hooks"]
		if !nested {
			arrays := true
			for _, value := range desired {
				if trimmed := bytes.TrimSpace(value); len(trimmed) == 0 || trimmed[0] != '[' {
					arrays = false
					break
				}
			}
			if !arrays {
				continue
			}
		}
		file := filepath.Join(outDir, filepath.FromSlash(a.Path))
		existing, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		doc, err := jsonc.Parse(file, existing)
		if err != nil {
			return nil, err
		}
		if doc.Root().Kind() != jsonc.KindObject {
			return nil, fmt.Errorf("hook aggregate must be a JSON object: %s", file)
		}
		rawDesired := a.Bytes
		if !nested {
			wrapped, err := json.Marshal(map[string]json.RawMessage{"hooks": doc.Root().Canonical()})
			if err != nil {
				return nil, err
			}
			doc, err = jsonc.Parse(file, wrapped)
			if err != nil {
				return nil, err
			}
			desired = map[string]json.RawMessage{"hooks": a.Bytes}
			rawDesired, err = json.Marshal(desired)
			if err != nil {
				return nil, err
			}
		}
		retained, err := retainNativeManagedEntries(doc, desired, rawDesired)
		if err != nil {
			return nil, err
		}
		if !nested {
			var unwrapped map[string]json.RawMessage
			if err := json.Unmarshal(retained, &unwrapped); err != nil {
				return nil, err
			}
			retained = unwrapped["hooks"]
		}
		result[i].Bytes = retained
	}
	return result, nil
}
