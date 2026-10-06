package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
	file, mode, size, err := openHookSupportFileAt(dirFD, relative, *total)
	if err != nil {
		return nil, 0, err
	}
	data, readErr := readFingerprintFD(file, relative, total)
	closeErr := file.Close()
	if readErr != nil {
		return nil, 0, readErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	if int64(len(data)) != size {
		return nil, 0, fmt.Errorf("hook support file changed while reading")
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

func hookSupportArtifactsFromSnapshot(res resolver.ResolvedArtifact, snapshot *hookSourceSnapshot) ([]artifact.Artifact, error) {
	if snapshot == nil {
		return nil, errors.New("hook source snapshot is unavailable")
	}
	excluded, inventoryDir := hookSupportExclusions(res, snapshot.definitionDir)
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

func preparedHookSourceDigest(res resolver.ResolvedArtifact, target hcc.Target, machineScope bool, snapshot *hookSourceSnapshot) (string, error) {
	hook, ok := res.Resource.(*ir.Hook)
	if !ok {
		return "", errors.New("hook source proof requires a hook resource")
	}
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror {
		return "", nil
	}
	binding := hook.SourceDefinitionDigest()
	if binding != "" {
		if snapshot == nil || snapshot.definitionRel == "" {
			return "", errors.New("hook source definition snapshot is unavailable")
		}
		sum := sha256.Sum256(snapshot.definitionData)
		if binding != "sha256:"+hex.EncodeToString(sum[:]) {
			return "", errors.New("hook source definition changed after ingestion")
		}
		input, err := nativeHookFingerprintInput(res, target, machineScope)
		if err != nil {
			return "", err
		}
		return hookSourceFingerprintFromSnapshot(input, snapshot)
	}
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionIndexOnly {
		if err := verifyIndexOnlyHookDefinition(res, snapshot); err != nil {
			return "", err
		}
		input, err := nativeHookFingerprintInput(res, target, machineScope)
		if err != nil {
			return "", err
		}
		return hookSourceFingerprintFromSnapshot(input, snapshot)
	}
	return "", nil
}

func verifyIndexOnlyHookDefinition(res resolver.ResolvedArtifact, snapshot *hookSourceSnapshot) error {
	original, ok := res.Resource.(*ir.Hook)
	if !ok || snapshot == nil || snapshot.definitionRel == "" {
		return errors.New("index-only hook definition snapshot is unavailable")
	}
	sourceFile := original.Source.File
	if sourceFile == "" || sourceFile == "." || strings.Contains(sourceFile, "\\") || filepath.IsAbs(sourceFile) || path.IsAbs(sourceFile) || !fs.ValidPath(sourceFile) || path.Clean(sourceFile) != sourceFile {
		return errors.New("index-only hook source file is unavailable")
	}
	sourceHarness, err := indexOnlyHookSourceHarness(res, original)
	if err != nil {
		return err
	}
	bundle, err := hcc.IngestBundle(hookDefinitionSnapshotFS{definition: sourceFile, data: snapshot.definitionData}, sourceHarness)
	if err != nil {
		return errors.New("index-only hook definition could not be re-ingested")
	}
	ingested, err := selectIndexOnlyHook(bundle, original.Name())
	if err != nil {
		return err
	}
	want, err := canonicalHookPayload(original)
	if err != nil {
		return errors.New("index-only hook payload could not be canonicalized")
	}
	got, err := canonicalHookPayload(ingested)
	if err != nil {
		return errors.New("re-ingested index-only hook payload could not be canonicalized")
	}
	if !bytes.Equal(want, got) {
		return errors.New("captured index-only hook definition differs from the decoded resource")
	}
	return nil
}

func indexOnlyHookSourceHarness(res resolver.ResolvedArtifact, hook *ir.Hook) (hcc.Harness, error) {
	source := hook.HookSource
	if source == "" {
		source = string(hcc.TargetClaude)
	}
	if res.SourceHarness != "" && res.SourceHarness != source {
		return "", errors.New("index-only hook source harness conflicts with canonical provenance")
	}
	if _, ok := ingest.For(ingest.Harness(source)); !ok {
		return "", errors.New("index-only hook source harness is unavailable")
	}
	return hcc.Harness(source), nil
}

func selectIndexOnlyHook(bundle *ingest.Bundle, name string) (*ir.Hook, error) {
	if bundle == nil {
		return nil, errors.New("index-only hook ingestion returned no bundle")
	}
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Severity == diag.SeverityError {
			return nil, errors.New("index-only hook ingestion reported an error")
		}
	}
	var match *ir.Hook
	for _, resource := range bundle.Resources {
		hook, ok := resource.(*ir.Hook)
		if !ok || hook.Name() != name {
			continue
		}
		if match != nil {
			return nil, errors.New("index-only hook source name is ambiguous")
		}
		match = hook
	}
	if match == nil {
		return nil, errors.New("index-only hook source name is missing")
	}
	return match, nil
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
