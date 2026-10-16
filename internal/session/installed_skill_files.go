package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

var ErrInstalledSkillFileUnsupported = errors.New("installed skill file preview is unsupported")

const installedSkillPrimaryPath = "SKILL.md"

type InstalledSkillFile struct {
	Path            string
	Identity        string
	MediaType       string
	InspectionClass string
	Size            int64
}

type installedSkillMember struct {
	file   InstalledSkillFile
	data   []byte
	staged bool
	state  fsboundary.Identity
}

type InstalledSkillBundle struct {
	files   []InstalledSkillFile
	members map[string]installedSkillMember
	dir     *fsboundary.Dir
	closed  bool
}

func MarketplaceInstalledSkillFiles(row Row) bool {
	return row.Artifact.IsMarketplace &&
		row.Artifact.Kind == ir.KindSkill &&
		row.MarketplaceDiscovery == nil &&
		!row.PendingVerification
}

func OpenInstalledSkillBundle(row Row) (*InstalledSkillBundle, error) {
	if !MarketplaceInstalledSkillFiles(row) {
		return nil, errors.New("resource is not an installed Marketplace skill")
	}
	if row.Artifact.Path == "" {
		return nil, errors.New("installed Marketplace skill has no verified local content")
	}
	info, err := os.Lstat(row.Artifact.Path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("installed Marketplace skill source is a symlink")
	}
	if info.Mode().IsRegular() {
		if filepath.Base(row.Artifact.Path) != marketplaceArtifactName {
			return nil, errors.New("installed Marketplace skill archive has an unexpected path")
		}
		return openInstalledSkillArchive(row, row.Artifact.Path)
	}
	if info.IsDir() {
		return openInstalledStagedSkill(row, row.Artifact.Path)
	}
	return nil, errors.New("installed Marketplace skill source is not an archive or directory")
}

func (b *InstalledSkillBundle) Files() []InstalledSkillFile {
	if b == nil {
		return nil
	}
	return append([]InstalledSkillFile(nil), b.files...)
}

func (b *InstalledSkillBundle) PrimaryPath() string { return installedSkillPrimaryPath }

func (b *InstalledSkillBundle) ReadFile(path string) ([]byte, error) {
	if b == nil || b.closed {
		return nil, errors.New("installed skill files are unavailable")
	}
	member, ok := b.members[path]
	if !ok {
		return nil, errors.New("installed skill file is not in the verified bundle")
	}
	if member.file.InspectionClass != "text" {
		return nil, ErrInstalledSkillFileUnsupported
	}
	if !member.staged {
		if !inspection.IsValidText(member.data) {
			return nil, ErrInstalledSkillFileUnsupported
		}
		return append([]byte(nil), member.data...), nil
	}
	if b.dir == nil {
		return nil, errors.New("installed skill files are unavailable")
	}
	file, current, err := b.dir.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	currentIdentity, err := installedStageIdentity(current)
	if err != nil {
		return nil, err
	}
	if !current.SameState(member.state) || currentIdentity != member.file.Identity {
		return nil, fmt.Errorf("%w: installed skill file changed since listing", fsboundary.ErrGenerationConflict)
	}
	data, readIdentity, err := fsboundary.ReadFile(file, hccartifact.DefaultArchiveLimits().MaxAssetBytes)
	if err != nil {
		return nil, err
	}
	readIdentityHash, err := installedStageIdentity(readIdentity)
	if err != nil {
		return nil, err
	}
	if !readIdentity.SameState(current) || readIdentityHash != member.file.Identity {
		return nil, fmt.Errorf("%w: installed skill file changed while reading", fsboundary.ErrGenerationConflict)
	}
	if !inspection.IsValidText(data) {
		return nil, ErrInstalledSkillFileUnsupported
	}
	return append([]byte(nil), data...), nil
}

func (b *InstalledSkillBundle) Close() error {
	if b == nil || b.dir == nil || b.closed {
		return nil
	}
	b.closed = true
	dir := b.dir
	b.dir = nil
	return dir.Close()
}

func openInstalledSkillArchive(row Row, path string) (*InstalledSkillBundle, error) {
	canonical, err := ReadVerifiedMarketplaceArtifact(path)
	if err != nil {
		return nil, err
	}
	if canonical.Manifest.Kind != ir.KindSkill.String() {
		return nil, errors.New("installed Marketplace archive is not a skill")
	}
	expectedName := installedSkillExpectedName(row)
	if canonical.Manifest.Name != expectedName {
		return nil, errors.New("installed Marketplace archive name does not match the resource")
	}
	if row.Artifact.MarketplaceArchiveDigest != "" && row.Artifact.MarketplaceArchiveDigest != canonical.Manifest.ArchiveDigest {
		return nil, errors.New("installed Marketplace archive digest does not match the resource")
	}
	resource, err := hccartifact.DecodeResource(canonical.Manifest.Kind, canonical.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode installed Marketplace skill: %w", err)
	}
	skill, ok := resource.(*ir.Skill)
	if !ok || skill == nil || skill.Name() != canonical.Manifest.Name {
		return nil, errors.New("installed Marketplace archive payload does not match the skill manifest")
	}
	declared, err := installedSkillSupportPaths(skill)
	if err != nil {
		return nil, err
	}
	if err := validateInstalledArchiveSupport(declared, canonical.Support); err != nil {
		return nil, err
	}
	bundle := newInstalledSkillBundle()
	manifest, err := json.MarshalIndent(canonical.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode installed Marketplace manifest: %w", err)
	}
	if err := bundle.addArchiveFile(hccartifact.CanonicalManifestPath, manifest); err != nil {
		return nil, err
	}
	for name, data := range canonical.Assets {
		if err := bundle.addArchiveFile(hccartifact.CanonicalAssetsDir+"/"+name, data); err != nil {
			return nil, err
		}
	}
	for name, data := range canonical.Licenses {
		if err := bundle.addArchiveFile(hccartifact.CanonicalLicensesDir+"/"+name, data); err != nil {
			return nil, err
		}
	}
	markdown, err := CompileSkillMarkdown(skill)
	if err != nil {
		return nil, fmt.Errorf("compile installed Marketplace skill: %w", err)
	}
	if err := bundle.addArchiveFile(installedSkillPrimaryPath, markdown); err != nil {
		return nil, err
	}
	for _, supportPath := range declared {
		if err := bundle.addArchiveFile(supportPath, canonical.Support[supportPath].Data); err != nil {
			return nil, err
		}
	}
	bundle.sortFiles()
	return bundle, nil
}

func openInstalledStagedSkill(row Row, path string) (*InstalledSkillBundle, error) {
	skill, ok := row.Artifact.Resource.(*ir.Skill)
	if !ok || skill == nil {
		return nil, errors.New("installed staged Marketplace skill has no verified skill definition")
	}
	if skill.Name() != installedSkillExpectedName(row) {
		return nil, errors.New("installed staged Marketplace skill name does not match the resource")
	}
	root, err := installedSkillSourceRoot(path)
	if err != nil {
		return nil, err
	}
	dir, err := fsboundary.OpenDir(root)
	if err != nil {
		return nil, err
	}
	bundle := newInstalledSkillBundle()
	bundle.dir = dir
	fail := func(err error) (*InstalledSkillBundle, error) {
		_ = dir.Close()
		return nil, err
	}
	primary, primaryIdentity, err := dir.OpenFile(installedSkillPrimaryPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("open staged skill primary: %w", err))
		}
		markdown, err := CompileSkillMarkdown(skill)
		if err != nil {
			return fail(fmt.Errorf("compile staged Marketplace skill: %w", err))
		}
		if err := bundle.addArchiveFile(installedSkillPrimaryPath, markdown); err != nil {
			return fail(err)
		}
	} else {
		metadata, err := installedStageFile(installedSkillPrimaryPath, primaryIdentity)
		closeErr := primary.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		bundle.addStagedFile(metadata, primaryIdentity)
	}
	declared, err := installedSkillSupportPaths(skill)
	if err != nil {
		return fail(err)
	}
	for _, supportPath := range declared {
		if supportPath == installedSkillPrimaryPath {
			return fail(errors.New("installed staged support duplicates the canonical skill primary"))
		}
		file, identity, err := dir.OpenFile(supportPath)
		if err != nil {
			return fail(fmt.Errorf("open staged skill support file: %w", err))
		}
		metadata, metadataErr := installedStageFile(supportPath, identity)
		closeErr := file.Close()
		if metadataErr != nil {
			return fail(metadataErr)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		bundle.addStagedFile(metadata, identity)
	}
	bundle.sortFiles()
	return bundle, nil
}

func installedSkillExpectedName(row Row) string {
	if row.Artifact.MarketplaceResourceName != "" {
		return row.Artifact.MarketplaceResourceName
	}
	if row.Artifact.Resource != nil {
		return row.Artifact.Resource.Name()
	}
	return row.Artifact.Name
}

func installedSkillSupportPaths(skill *ir.Skill) ([]string, error) {
	paths := make([]string, 0, len(skill.SupportFiles))
	seen := make(map[string]struct{}, len(skill.SupportFiles))
	for _, path := range skill.SupportFiles {
		if err := hccartifact.ValidateSupportPath(path); err != nil {
			return nil, fmt.Errorf("invalid declared support file: %w", err)
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func validateInstalledArchiveSupport(declared []string, support map[string]hccartifact.SupportFile) error {
	if len(declared) != len(support) {
		return errors.New("installed Marketplace archive support files do not match the skill definition")
	}
	for path := range support {
		if _, ok := slices.BinarySearch(declared, path); !ok {
			return errors.New("installed Marketplace archive contains undeclared support files")
		}
	}
	for _, path := range declared {
		if _, ok := support[path]; !ok {
			return errors.New("installed Marketplace archive is missing declared support files")
		}
	}
	return nil
}

func installedSkillSourceRoot(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("installed staged skill root is not a regular directory")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve installed staged skill root: %w", err)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve installed staged skill root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", errors.New("installed staged skill root is not a regular directory")
	}
	return root, nil
}

func newInstalledSkillBundle() *InstalledSkillBundle {
	return &InstalledSkillBundle{members: make(map[string]installedSkillMember)}
}

func (b *InstalledSkillBundle) addArchiveFile(path string, data []byte) error {
	if _, exists := b.members[path]; exists {
		return errors.New("installed Marketplace bundle contains duplicate file paths")
	}
	copyData := append([]byte(nil), data...)
	mediaType, inspectionClass := installedSkillFileType(path)
	file := InstalledSkillFile{
		Path:            path,
		Identity:        hccartifact.ContentDigest(copyData),
		MediaType:       mediaType,
		InspectionClass: inspectionClass,
		Size:            int64(len(copyData)),
	}
	b.members[path] = installedSkillMember{file: file, data: copyData}
	b.files = append(b.files, file)
	return nil
}

func (b *InstalledSkillBundle) addStagedFile(file InstalledSkillFile, identity fsboundary.Identity) {
	b.members[file.Path] = installedSkillMember{file: file, staged: true, state: identity}
	b.files = append(b.files, file)
}

func (b *InstalledSkillBundle) sortFiles() {
	sort.Slice(b.files, func(i, j int) bool { return b.files[i].Path < b.files[j].Path })
}

func installedStageFile(path string, identity fsboundary.Identity) (InstalledSkillFile, error) {
	identityHash, err := installedStageIdentity(identity)
	if err != nil {
		return InstalledSkillFile{}, err
	}
	mediaType, inspectionClass := installedSkillFileType(path)
	return InstalledSkillFile{
		Path:            path,
		Identity:        identityHash,
		MediaType:       mediaType,
		InspectionClass: inspectionClass,
		Size:            identity.Size,
	}, nil
}

func installedStageIdentity(identity fsboundary.Identity) (string, error) {
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode staged file identity: %w", err)
	}
	sum := sha256.Sum256(identityJSON)
	return hex.EncodeToString(sum[:]), nil
}

func installedSkillFileType(path string) (mediaType, inspectionClass string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", "image"
	case ".jpg", ".jpeg":
		return "image/jpeg", "image"
	case ".gif":
		return "image/gif", "image"
	case ".webp":
		return "image/webp", "image"
	case ".bmp":
		return "image/bmp", "image"
	case ".tif", ".tiff":
		return "image/tiff", "image"
	case ".ico":
		return "image/x-icon", "image"
	case ".md":
		return "text/markdown", "text"
	default:
		return "text/plain", "text"
	}
}
