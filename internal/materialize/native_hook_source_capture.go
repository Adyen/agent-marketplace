package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type hookSourceSnapshot struct {
	root           string
	definitionRel  string
	definitionDir  string
	definitionData []byte
	files          []hookSourceFingerprintFile
}

func captureHookSourceSnapshot(sourceRoot, definition, definitionDir string, excludedPeers ...string) (*hookSourceSnapshot, error) {
	if !filepath.IsAbs(sourceRoot) {
		return nil, fmt.Errorf("hook support source root is not absolute")
	}
	if definitionDir != "" && definitionDir != "." && (!fs.ValidPath(definitionDir) || path.IsAbs(definitionDir) || path.Clean(definitionDir) != definitionDir) {
		return nil, fmt.Errorf("hook support definition directory is invalid")
	}
	if definition != "" && (definition == "." || !fs.ValidPath(definition) || path.IsAbs(definition) || path.Clean(definition) != definition || path.Dir(definition) != definitionDir) {
		return nil, fmt.Errorf("hook support definition path is invalid")
	}
	root := filepath.Clean(sourceRoot)
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open hook source root: %w", err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	dirFD, err := openHookSupportDirAt(rootFD, definitionDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirFD) }()

	definitionName := ""
	if definition != "" {
		definitionName = path.Base(definition)
	}
	if err := validateHookSupportPeersAt(dirFD, excludedPeers, definitionName); err != nil {
		return nil, err
	}
	snapshot := &hookSourceSnapshot{
		root:          root,
		definitionRel: definition,
		definitionDir: definitionDir,
		files:         make([]hookSourceFingerprintFile, 0),
	}
	var total int64
	if definition != "" {
		data, _, err := readHookSourceSnapshotFile(dirFD, definitionName, &total)
		if err != nil {
			return nil, err
		}
		snapshot.definitionData = data
	}
	if err := walkFingerprintDir(dirFD, func(relative string) error {
		if relative == definitionName && definitionName != "" {
			return nil
		}
		data, mode, err := readHookSourceSnapshotFile(dirFD, relative, &total)
		if err != nil {
			return err
		}
		snapshot.files = append(snapshot.files, hookSourceFingerprintFile{rel: relative, mode: mode, data: data})
		return nil
	}); err != nil {
		return nil, err
	}
	if err := validateHookSupportPeersAt(dirFD, excludedPeers, definitionName); err != nil {
		return nil, err
	}
	sort.Slice(snapshot.files, func(i, j int) bool { return snapshot.files[i].rel < snapshot.files[j].rel })
	return snapshot, nil
}

func readHookSourceSnapshotFile(dirFD int, relative string, total *int64) ([]byte, os.FileMode, error) {
	return readHookSourceSnapshotFileChecked(dirFD, relative, total, nil)
}

// readHookSourceSnapshotFileChecked reads one captured support file while
// proving the opened descriptor remains unchanged before and after the read.
// The state comparison includes device, inode, size, modification and change
// times (ctime), link count, mode, and owner. The between hook is a
// deterministic test seam that runs after descriptor validation and before
// its bytes are read.
func readHookSourceSnapshotFileChecked(dirFD int, relative string, total *int64, between func(*os.File)) ([]byte, os.FileMode, error) {
	file, mode, size, err := openHookSupportFileAt(dirFD, relative, *total)
	if err != nil {
		return nil, 0, err
	}
	before, err := fsboundary.FileIdentity(file)
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if before.Size != size || before.Mode != mode.Perm() {
		_ = file.Close()
		return nil, 0, fmt.Errorf("%w: %w: hook support file changed during admission", dependencytxn.ErrGenerationConflict, fsboundary.ErrGenerationConflict)
	}
	if before.Links != 1 {
		_ = file.Close()
		return nil, 0, fmt.Errorf("%w: hook support file has multiple hard links", fsboundary.ErrUnsafeFile)
	}
	if between != nil {
		between(file)
	}
	data, readErr := readFingerprintFD(file, relative, total)
	if readErr != nil {
		_ = file.Close()
		return nil, 0, readErr
	}
	after, statErr := fsboundary.FileIdentity(file)
	closeErr := file.Close()
	if statErr != nil {
		return nil, 0, statErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	if int64(len(data)) != size || !before.SameState(after) {
		return nil, 0, fmt.Errorf("%w: %w: hook support file changed while reading", dependencytxn.ErrGenerationConflict, fsboundary.ErrGenerationConflict)
	}
	return data, mode.Perm(), nil
}

func validateHookSupportPeersAt(dirFD int, peers []string, definitionName string) error {
	for _, peer := range peers {
		if peer == definitionName {
			continue
		}
		file, _, _, err := openHookSupportFileAt(dirFD, peer, 0)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
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
	if !fs.ValidPath(relative) {
		return nil, 0, 0, fmt.Errorf("hook support file path is invalid")
	}
	components := strings.Split(relative, "/")
	currentFD := dirFD
	ownedFD := -1
	for _, component := range components[:len(components)-1] {
		nextFD, err := openFingerprintDir(currentFD, component)
		if ownedFD >= 0 {
			_ = unix.Close(ownedFD)
		}
		if err != nil {
			return nil, 0, 0, err
		}
		currentFD = nextFD
		ownedFD = nextFD
	}
	if ownedFD >= 0 {
		defer func() { _ = unix.Close(ownedFD) }()
	}
	file, _, err := openFingerprintFile(currentFD, components[len(components)-1])
	if err != nil {
		return nil, 0, 0, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		_ = file.Close()
		return nil, 0, 0, fmt.Errorf("stat hook support file: %w", err)
	}
	if stat.Nlink != 1 {
		_ = file.Close()
		return nil, 0, 0, fmt.Errorf("%w: hook support file has multiple hard links", fsboundary.ErrUnsafeFile)
	}
	remaining := int64(hookSourceFingerprintMaxBytes) - total
	if stat.Size < 0 || remaining < 0 || stat.Size > remaining {
		_ = file.Close()
		return nil, 0, 0, fmt.Errorf("%w: hook support source exceeds the existing size limit", fsboundary.ErrTooLarge)
	}
	return file, fs.FileMode(stat.Mode & 0o777), stat.Size, nil
}

func hookSupportArtifactsFromSnapshot(res resolver.ResolvedArtifact, snapshot *hookSourceSnapshot) ([]artifact.Artifact, error) {
	if snapshot == nil {
		return nil, errors.New("hook source snapshot is unavailable")
	}
	excluded, inventoryDir := nativeHookSourceExclusions(res, snapshot.definitionDir)
	definitionName := ""
	if snapshot.definitionRel != "" {
		definitionName = path.Base(snapshot.definitionRel)
	}
	files := make([]artifact.Artifact, 0, len(snapshot.files))
	for _, source := range snapshot.files {
		if source.rel == definitionName || excluded[source.rel] {
			continue
		}
		for peer := range excluded {
			if strings.HasPrefix(source.rel, peer+"/") {
				return nil, fmt.Errorf("native hook aggregate peer is not a regular file")
			}
		}
		if inventoryDir && !strings.Contains(source.rel, "/") && strings.HasSuffix(source.rel, ".json") {
			continue
		}
		file := artifact.Artifact{Path: source.rel, Bytes: bytes.Clone(source.data), Mode: source.mode.Perm()}
		if err := file.Validate(); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// nativeHookSourceExclusions lists the source-harness aggregate basenames the
// captured definition directory must not re-emit as hook support: the
// definition itself plus its declared aggregate siblings. The second result
// marks a Windsurf hooks inventory directory, whose flat .json siblings are
// per-hook definitions rather than support.
func nativeHookSourceExclusions(res resolver.ResolvedArtifact, definitionDir string) (map[string]bool, bool) {
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
// locations for project and user scopes relative to the source root, resolved
// through the source target's registered native layout.
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
				paths = append(paths, path.Join(definition.RepoRoot, partition.Prefix, file.Path))
			}
		}
	}
	return paths
}

func hookSourceFingerprintFromSnapshot(input HookSourceFingerprintInput, snapshot *hookSourceSnapshot) (string, error) {
	if err := validateHookSourceFingerprintInput(input); err != nil {
		return "", err
	}
	if snapshot == nil || snapshot.definitionRel == "" {
		return "", errors.New("hook source definition snapshot is unavailable")
	}
	if !filepath.IsAbs(input.SourceRoot) || !filepath.IsAbs(snapshot.root) {
		return "", errors.New("hook source fingerprint root is not absolute")
	}
	inputDefinition, err := filepath.Abs(filepath.Join(input.SourceRoot, filepath.FromSlash(input.DefinitionRel)))
	if err != nil {
		return "", err
	}
	capturedDefinition, err := filepath.Abs(filepath.Join(snapshot.root, filepath.FromSlash(snapshot.definitionRel)))
	if err != nil {
		return "", err
	}
	if filepath.Clean(inputDefinition) != filepath.Clean(capturedDefinition) {
		return "", errors.New("hook source fingerprint definition differs from captured source")
	}
	return serializeHookSourceFingerprint(input, snapshot.definitionData, snapshot.files), nil
}

func canonicalHookPayload(hook *ir.Hook) ([]byte, error) {
	copyHook := *hook
	copyHook.Source = ir.SourceRef{}
	canonical, err := artifact.FromIR(&copyHook)
	if err != nil {
		return nil, err
	}
	return canonical.Payload, nil
}

type hookDefinitionSnapshotFS struct {
	definition string
	data       []byte
}

func (source hookDefinitionSnapshotFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == source.definition {
		return &hookDefinitionSnapshotFile{Reader: bytes.NewReader(source.data), info: hookDefinitionSnapshotInfo{name: path.Base(name), size: int64(len(source.data)), mode: 0o444}}, nil
	}
	entries, ok := source.readDirEntries(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &hookDefinitionSnapshotDir{info: hookDefinitionSnapshotInfo{name: path.Base(name), mode: fs.ModeDir | 0o555}, entries: entries}, nil
}

func (source hookDefinitionSnapshotFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	if name == source.definition {
		return bytes.Clone(source.data), nil
	}
	if name == "." || source.isDirectory(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
}

func (source hookDefinitionSnapshotFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if name == source.definition {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	entries, ok := source.readDirEntries(name)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return entries, nil
}

func (source hookDefinitionSnapshotFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if name == source.definition {
		return hookDefinitionSnapshotInfo{name: path.Base(name), size: int64(len(source.data)), mode: 0o444}, nil
	}
	if name == "." || source.isDirectory(name) {
		return hookDefinitionSnapshotInfo{name: path.Base(name), mode: fs.ModeDir | 0o555}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (source hookDefinitionSnapshotFS) isDirectory(name string) bool {
	return name != "" && name != source.definition && fs.ValidPath(name) && strings.HasPrefix(source.definition, name+"/")
}

func (source hookDefinitionSnapshotFS) readDirEntries(name string) ([]fs.DirEntry, bool) {
	if name != "." && !source.isDirectory(name) {
		return nil, false
	}
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	if !strings.HasPrefix(source.definition, prefix) {
		return nil, false
	}
	remainder := strings.TrimPrefix(source.definition, prefix)
	if remainder == "" {
		return nil, false
	}
	child, _, nested := strings.Cut(remainder, "/")
	mode := fs.FileMode(0o444)
	size := int64(len(source.data))
	if nested {
		mode = fs.ModeDir | 0o555
		size = 0
	}
	return []fs.DirEntry{hookDefinitionSnapshotEntry{info: hookDefinitionSnapshotInfo{name: child, size: size, mode: mode}}}, true
}

type hookDefinitionSnapshotFile struct {
	*bytes.Reader
	info hookDefinitionSnapshotInfo
}

func (file *hookDefinitionSnapshotFile) Close() error { return nil }

func (file *hookDefinitionSnapshotFile) Stat() (fs.FileInfo, error) { return file.info, nil }

type hookDefinitionSnapshotDir struct {
	info    hookDefinitionSnapshotInfo
	entries []fs.DirEntry
	offset  int
}

func (dir *hookDefinitionSnapshotDir) Close() error { return nil }

func (dir *hookDefinitionSnapshotDir) Stat() (fs.FileInfo, error) { return dir.info, nil }

func (dir *hookDefinitionSnapshotDir) Read([]byte) (int, error) {
	return 0, errors.New("cannot read a directory")
}

func (dir *hookDefinitionSnapshotDir) ReadDir(count int) ([]fs.DirEntry, error) {
	if count <= 0 {
		entries := append([]fs.DirEntry(nil), dir.entries[dir.offset:]...)
		dir.offset = len(dir.entries)
		return entries, nil
	}
	if dir.offset >= len(dir.entries) {
		return nil, io.EOF
	}
	end := len(dir.entries)
	if dir.offset+count < end {
		end = dir.offset + count
	}
	entries := append([]fs.DirEntry(nil), dir.entries[dir.offset:end]...)
	dir.offset = end
	return entries, nil
}

type hookDefinitionSnapshotInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (info hookDefinitionSnapshotInfo) Name() string       { return info.name }
func (info hookDefinitionSnapshotInfo) Size() int64        { return info.size }
func (info hookDefinitionSnapshotInfo) Mode() fs.FileMode  { return info.mode }
func (info hookDefinitionSnapshotInfo) ModTime() time.Time { return time.Time{} }
func (info hookDefinitionSnapshotInfo) IsDir() bool        { return info.mode.IsDir() }
func (info hookDefinitionSnapshotInfo) Sys() any           { return nil }

type hookDefinitionSnapshotEntry struct {
	info hookDefinitionSnapshotInfo
}

func (entry hookDefinitionSnapshotEntry) Name() string               { return entry.info.Name() }
func (entry hookDefinitionSnapshotEntry) IsDir() bool                { return entry.info.IsDir() }
func (entry hookDefinitionSnapshotEntry) Type() fs.FileMode          { return entry.info.Mode().Type() }
func (entry hookDefinitionSnapshotEntry) Info() (fs.FileInfo, error) { return entry.info, nil }
