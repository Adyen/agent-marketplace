package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const codexTargetName = string(targetmeta.TargetCodex)

// CodexPartition is one independently rooted half of a machine Codex backup.
type CodexPartition struct {
	ID       string
	Root     string
	RelPaths []string
}

type codexSetManifest struct {
	Version    int                         `json:"version"`
	Operation  string                      `json:"operation"`
	Partitions []codexSetManifestPartition `json:"partitions"`
	observed   fsboundary.Identity
}

type codexSetManifestPartition struct {
	ID       string   `json:"id"`
	RootKey  string   `json:"root_key"`
	Archive  string   `json:"archive"`
	RelPaths []string `json:"managed_paths"`
}

// CreateCodexSet publishes one logical backup containing both native roots.
func CreateCodexSet(partitions []CodexPartition) (operation string, err error) {
	slog.Debug("creating Codex backup set", "partitions", len(partitions))
	defer func() {
		if err != nil {
			slog.Error("Codex backup set creation failed", "operation", operation, "error", err)
		}
	}()
	if err := validateCodexPartitions(partitions); err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(dir, codexTargetName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("backup: create Codex set directory: %w", err)
	}
	parent, err := fsboundary.OpenDir(targetDir)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	output := codexSetOutput{parent: parent, native: native, path: targetDir, owned: make(map[string]fsboundary.Identity)}
	defer func() {
		if err != nil {
			err = errors.Join(err, output.cleanup())
		}
	}()
	operation, err = nextCodexOperation(native, time.Now().Format("2006-01-02-150405"))
	if err != nil {
		return "", err
	}
	manifest := codexSetManifest{Version: 1, Operation: operation}
	var staged []string
	for _, partition := range partitions {
		canonical, err := codexCanonicalRoot(partition.Root)
		if err != nil {
			return "", err
		}
		existing, err := existingPaths(partition.Root, partition.RelPaths)
		if err != nil {
			return "", fmt.Errorf("backup: scan Codex partition %s: %w", partition.ID, err)
		}
		rootKey := codexRootKey(canonical)
		archive := fmt.Sprintf("%s-%s-%s.tar.gz", operation, partition.ID, rootKey)
		stagedName := archive + ".partial"
		if err := output.write(stagedName, 0o600, func(file *os.File) error { return writeBackupSourceArchive(file, partition.Root, existing) }); err != nil {
			return "", fmt.Errorf("backup: archive Codex partition %s: %w", partition.ID, err)
		}
		staged = append(staged, stagedName)
		manifest.Partitions = append(manifest.Partitions, codexSetManifestPartition{ID: partition.ID, RootKey: rootKey, Archive: archive, RelPaths: append([]string(nil), partition.RelPaths...)})
	}
	sort.Slice(manifest.Partitions, func(i, j int) bool { return manifest.Partitions[i].ID < manifest.Partitions[j].ID })
	for _, name := range staged {
		if err := output.publish(name, strings.TrimSuffix(name, ".partial")); err != nil {
			return "", fmt.Errorf("backup: publish Codex archive: %w", err)
		}
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	manifestName := operation + ".set.json"
	if err := output.write(manifestName+".partial", 0o644, func(file *os.File) error { _, writeErr := file.Write(append(raw, '\n')); return writeErr }); err != nil {
		return "", err
	}
	if err := output.publish(manifestName+".partial", manifestName); err != nil {
		return "", err
	}
	slog.Info("Codex backup set created", "operation", operation, "partitions", len(partitions))
	return operation, nil
}

// RestoreCodexSet restores the newest complete set matching the current roots.
func RestoreCodexSet(partitions []CodexPartition) (operation string, err error) {
	slog.Debug("restoring Codex backup set", "partitions", len(partitions))
	defer func() {
		if err != nil {
			slog.Error("Codex backup set restore failed", "operation", operation, "error", err)
		}
	}()
	if err := validateCodexPartitions(partitions); err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(dir, codexTargetName)
	parent, err := fsboundary.OpenDir(targetDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return "", errors.New("backup: no complete Codex backup set matches the current canonical root keys")
		}
		return "", err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	manifests, err := loadCompleteCodexSetsAt(parent)
	if err != nil {
		return "", err
	}
	current := make(map[string]CodexPartition, len(partitions))
	keys := make(map[string]string, len(partitions))
	for _, partition := range partitions {
		canonical, err := codexCanonicalRoot(partition.Root)
		if err != nil {
			return "", err
		}
		current[partition.ID] = partition
		keys[partition.ID] = codexRootKey(canonical)
	}
	var selected *codexSetManifest
	for i := len(manifests) - 1; i >= 0; i-- {
		if codexManifestMatches(manifests[i], keys) {
			selected = &manifests[i]
			break
		}
	}
	if selected == nil {
		return "", errors.New("backup: no complete Codex backup set matches the current canonical root keys")
	}
	sources := make([]*fsboundary.Destination, len(selected.Partitions))
	defer func() {
		for _, source := range sources {
			if source != nil {
				if closeErr := source.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
			}
		}
	}()
	for i, archived := range selected.Partitions {
		partition := current[archived.ID]
		source, err := fsboundary.CaptureDestination(parent, archived.Archive, fsboundary.LinkPolicyRepository)
		if err != nil {
			return "", err
		}
		sources[i] = source
		if source.Link() != nil {
			return "", fsboundary.ErrUnsafeFile
		}
		if !source.Exists() {
			return "", fsboundary.ErrGenerationConflict
		}
		if err := source.ReadTargetWith(func(reader io.Reader) error {
			return validateCodexArchivePayload(reader, partition.Root, archived.RelPaths)
		}); err != nil {
			return "", fmt.Errorf("backup: preflight Codex partition %s: %w", archived.ID, err)
		}
	}
	slog.Debug("Codex backup set preflight completed", "operation", selected.Operation, "phase", "preflight-complete", "partitions", len(selected.Partitions))
	verify := func() error {
		if err := verifyArchiveParent(parent, targetDir); err != nil {
			return err
		}
		if _, err := inspectRetentionMember(parent, selected.Operation+".set.json", &selected.observed); err != nil {
			return err
		}
		for i, archived := range selected.Partitions {
			canonical, err := codexCanonicalRoot(current[archived.ID].Root)
			if err != nil {
				return err
			}
			if codexRootKey(canonical) != keys[archived.ID] {
				return fsboundary.ErrGenerationConflict
			}
			if err := sources[i].ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
				return err
			}
		}
		return nil
	}
	var completed []string
	for i, archived := range selected.Partitions {
		partition := current[archived.ID]
		if err := verify(); err != nil {
			return "", codexLateRestoreError(err, completed, selected.Partitions[i:])
		}
		if err := sources[i].ReadTargetWith(func(reader io.Reader) error {
			if err := os.MkdirAll(partition.Root, 0o755); err != nil {
				return err
			}
			return extractArchivePayload(reader, partition.Root)
		}); err != nil {
			return "", codexLateRestoreError(err, completed, selected.Partitions[i:])
		}
		completed = append(completed, archived.ID)
	}
	operation = selected.Operation
	slog.Info("Codex backup set restored", "operation", operation, "partitions", len(selected.Partitions))
	return operation, nil
}

func validateCodexPartitions(partitions []CodexPartition) error {
	want := codexNativePartitionCount()
	if want == 0 {
		return errors.New("backup: Codex native layout is not registered")
	}
	if len(partitions) != want {
		return fmt.Errorf("backup: Codex backup set requires exactly %d partitions, got %d", want, len(partitions))
	}
	seen := make(map[string]bool, want)
	for _, partition := range partitions {
		if !isCodexMachinePartitionID(partition.ID) {
			return fmt.Errorf("backup: unknown Codex partition %q", partition.ID)
		}
		if seen[partition.ID] || partition.Root == "" {
			return fmt.Errorf("backup: invalid Codex partition %q", partition.ID)
		}
		for _, rel := range partition.RelPaths {
			if !validCodexManagedPath(rel) {
				return fmt.Errorf("backup: invalid managed path %q in Codex partition %q", rel, partition.ID)
			}
		}
		seen[partition.ID] = true
	}
	return nil
}

func loadCompleteCodexSets(targetDir string) (sets []codexSetManifest, err error) {
	absolute, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(absolute)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return loadCompleteCodexSetsAt(root)
}

func loadCompleteCodexSetsAt(root *fsboundary.Dir) (sets []codexSetManifest, err error) {
	native, err := root.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	paths, err := fs.Glob(native.FS(), "*.set.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	sets = make([]codexSetManifest, 0, len(paths))
	for _, path := range paths {
		raw, observed, err := readCodexManifest(root, filepath.Base(path))
		if err != nil {
			return nil, err
		}
		var manifest codexSetManifest
		if json.Unmarshal(raw, &manifest) != nil || !validCodexSetManifest(manifest, filepath.Base(path)) {
			continue
		}
		complete := true
		for _, partition := range manifest.Partitions {
			if _, err := native.Stat(partition.Archive); err != nil {
				complete = false
			}
		}
		if complete {
			manifest.observed = observed
			sets = append(sets, manifest)
		}
	}
	return sets, nil
}

func readCodexManifest(root *fsboundary.Dir, name string) (data []byte, identity fsboundary.Identity, err error) {
	file, err := fsboundary.CaptureDestination(root, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, identity, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, identity, fsboundary.ErrUnsafeFile
	}
	return file.ReadTarget(file.Identity().Size)
}

func rotateCodexSets(root *fsboundary.Dir, name, targetDir string, maxDays, maxPerDay int) (err error) {
	parent, err := root.OpenPath(name)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	sets, err := loadCompleteCodexSetsAt(parent)
	if err != nil {
		return err
	}
	byDay := make(map[string][]codexSetManifest)
	var days []string
	for _, set := range sets {
		day := archiveDay(set.Operation)
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], set)
	}
	var remove []codexSetManifest
	for _, day := range days {
		group := byDay[day]
		if len(group) > maxPerDay {
			remove = append(remove, group[:len(group)-maxPerDay]...)
			byDay[day] = group[len(group)-maxPerDay:]
		}
	}
	if len(days) > maxDays {
		for _, day := range days[:len(days)-maxDays] {
			remove = append(remove, byDay[day]...)
		}
	}
	var names []string
	var identities []fsboundary.Identity
	for _, set := range remove {
		manifestName := set.Operation + ".set.json"
		if _, err := inspectRetentionMember(parent, manifestName, &set.observed); err != nil {
			return err
		}
		for _, partition := range set.Partitions {
			identity, err := inspectRetentionMember(parent, partition.Archive, nil)
			if err != nil {
				return err
			}
			names = append(names, partition.Archive)
			identities = append(identities, identity)
		}
		names = append(names, manifestName)
		identities = append(identities, set.observed)
	}
	for i, name := range names {
		if _, err := inspectRetentionMember(parent, name, &identities[i]); err != nil {
			return err
		}
	}
	index := 0
	for _, set := range remove {
		manifestName := set.Operation + ".set.json"
		for range set.Partitions {
			if _, err := inspectRetentionMember(parent, manifestName, &set.observed); err != nil {
				return err
			}
			if err := removeObservedBackups(parent, targetDir, names[index:index+1], identities[index:index+1]); err != nil {
				return err
			}
			index++
		}
		if err := removeObservedBackups(parent, targetDir, names[index:index+1], identities[index:index+1]); err != nil {
			return err
		}
		index++
	}
	return nil
}

func codexManifestMatches(manifest codexSetManifest, keys map[string]string) bool {
	if len(manifest.Partitions) != len(keys) {
		return false
	}
	for _, partition := range manifest.Partitions {
		if keys[partition.ID] != partition.RootKey {
			return false
		}
	}
	return true
}

func validCodexSetManifest(manifest codexSetManifest, filename string) bool {
	want := codexNativePartitionCount()
	if want == 0 || manifest.Version != 1 || len(manifest.Partitions) != want || manifest.Operation+".set.json" != filename {
		return false
	}
	seen := make(map[string]bool, want)
	for _, partition := range manifest.Partitions {
		if !isCodexMachinePartitionID(partition.ID) || seen[partition.ID] || filepath.Base(partition.Archive) != partition.Archive {
			return false
		}
		managed := make([]string, 0, len(partition.RelPaths))
		for _, rel := range partition.RelPaths {
			if !validCodexManagedPath(rel) {
				return false
			}
			managed = append(managed, filepath.Clean(rel))
		}
		sort.Strings(managed)
		for i, rel := range managed {
			if i > 0 && (rel == managed[i-1] || strings.HasPrefix(rel, managed[i-1]+string(filepath.Separator))) {
				return false
			}
		}
		seen[partition.ID] = true
	}
	return true
}

func codexNativePartitionCount() int {
	layout, _ := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	return len(layout.Partitions)
}

func isCodexMachinePartitionID(id string) bool {
	layout, _ := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	for _, partition := range layout.Partitions {
		if id == "machine-"+partition.ID {
			return true
		}
	}
	return false
}

func validCodexManagedPath(rel string) bool {
	clean := filepath.Clean(rel)
	return rel != "" && clean != "." && clean != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func validateCodexArchive(archivePath, root string, managed []string) error {
	return withBackupPayload(archivePath, func(reader io.Reader) error { return validateCodexArchivePayload(reader, root, managed) })
}

func validateCodexArchivePayload(payload io.Reader, root string, managed []string) error {
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return fmt.Errorf("destination root %s is not a real directory", root)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	gz, err := gzip.NewReader(payload)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := archiveTarget(root, header.Name)
		if err != nil {
			return err
		}
		if err := rejectSymlinkAncestors(root, target); err != nil {
			return err
		}
		if !codexManagedArchivePath(header.Name, managed) {
			return fmt.Errorf("archive entry %q is outside managed paths", header.Name)
		}
		if header.Typeflag == tar.TypeSymlink {
			if err := validateSymlinkTarget(header.Linkname); err != nil {
				return err
			}
		}
	}
}

func codexManagedArchivePath(name string, managed []string) bool {
	name = filepath.Clean(name)
	for _, allowed := range managed {
		allowed = filepath.Clean(allowed)
		if name == allowed || strings.HasPrefix(name, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func codexLateRestoreError(cause error, completed []string, pending []codexSetManifestPartition) error {
	remaining := make([]string, 0, len(pending))
	for _, partition := range pending {
		remaining = append(remaining, partition.ID)
	}
	return fmt.Errorf("backup: Codex restore failed after completed partitions %v; pending partitions %v: %w", completed, remaining, cause)
}

func nextCodexOperation(root *os.Root, timestamp string) (string, error) {
	for sequence := 0; ; sequence++ {
		operation := timestamp
		if sequence > 0 {
			operation += "-" + strconv.Itoa(sequence)
		}
		available := true
		for _, suffix := range []string{".set.json", ".set.json.partial"} {
			if _, err := root.Lstat(operation + suffix); err == nil {
				available = false
				break
			} else if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
		}
		if available {
			return operation, nil
		}
	}
}

func codexCanonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	remaining := []string{}
	current := abs
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(remaining) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, remaining[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs), nil
		}
		remaining = append(remaining, filepath.Base(current))
		current = parent
	}
}

func codexRootKey(canonical string) string {
	hash := sha256.New()
	hash.Write([]byte("agent-marketplace/ownership/root/v1"))
	hash.Write([]byte{0})
	hash.Write([]byte(strconv.Itoa(len(canonical))))
	hash.Write([]byte{0})
	hash.Write([]byte(canonical))
	return hex.EncodeToString(hash.Sum(nil))
}
