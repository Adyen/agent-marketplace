package packagegen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

type Input struct {
	Plugin             *ir.Plugin
	PackageRoot        string
	Target             hcc.Target
	Delivery           string
	Scope              string
	Exclusions         []string
	CatalogFingerprint string
}

type Generation struct {
	Key      string
	Path     string
	DataPath string
}

func Plan(input Input) (Generation, error) {
	if input.Plugin == nil || input.Plugin.Name() == "" || input.PackageRoot == "" || input.Target == "" {
		return Generation{}, errors.New("packagegen: plugin, package root, and target are required")
	}
	stateRoot, err := StateRoot()
	if err != nil {
		return Generation{}, err
	}
	key := BuildKey(input)
	instanceKey := InstanceKey(input.Plugin.Name(), input.Target, input.Scope)
	return Generation{
		Key:      key,
		Path:     filepath.Join(stateRoot, "packages", key),
		DataPath: filepath.Join(stateRoot, "plugin-data", instanceKey),
	}, nil
}

func StateRoot() (string, error) {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Dir(stateDir), nil
}

func BuildKey(input Input) string {
	h := sha256.New()
	writeIdentity(h, "agent-marketplace/package-generation/v1")
	writeIdentity(h, packageDigest(input.Plugin))
	writeIdentity(h, string(input.Target))
	writeIdentity(h, input.Delivery)
	writeIdentity(h, input.Scope)
	writeIdentity(h, input.CatalogFingerprint)
	exclusions := append([]string(nil), input.Exclusions...)
	slices.Sort(exclusions)
	exclusions = slices.Compact(exclusions)
	for _, exclusion := range exclusions {
		writeIdentity(h, exclusion)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func InstanceKey(plugin string, target hcc.Target, scope string) string {
	h := sha256.New()
	writeIdentity(h, "agent-marketplace/plugin-instance/v1")
	writeIdentity(h, plugin)
	writeIdentity(h, string(target))
	writeIdentity(h, scope)
	return hex.EncodeToString(h.Sum(nil))
}

func Publish(input Input) (Generation, error) {
	generation, err := Plan(input)
	if err != nil {
		return Generation{}, err
	}
	files, err := sortedFiles(input.Plugin)
	if err != nil {
		return Generation{}, err
	}
	packagesDir := filepath.Dir(generation.Path)
	if err := os.MkdirAll(packagesDir, 0o700); err != nil {
		return Generation{}, err
	}
	if err := requireDirectory(packagesDir); err != nil {
		return Generation{}, err
	}
	var result Generation
	err = dependencytxn.WithPathLock(context.Background(), packagesDir, func() error {
		if err := requireDirectory(packagesDir); err != nil {
			return err
		}
		if _, err := cleanupPackageStages(packagesDir, false); err != nil {
			return err
		}
		result, err = publishLocked(input, generation, files, packagesDir)
		return err
	})
	if err != nil {
		return Generation{}, err
	}
	return result, nil
}

func publishLocked(input Input, generation Generation, files []ir.PluginFile, packagesDir string) (result Generation, retErr error) {
	if existing, err := os.Stat(generation.Path); err == nil {
		if !existing.IsDir() {
			return Generation{}, fmt.Errorf("packagegen: generation %q is not a directory", generation.Key)
		}
		if err := verifyGeneration(generation.Path, input.PackageRoot, files); err != nil {
			return Generation{}, err
		}
		slog.Debug("plugin package generation reused", "generation_key", generation.Key, "target", input.Target)
		return generation, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, err
	}

	stage, err := os.MkdirTemp(packagesDir, ".packagegen-")
	if err != nil {
		return Generation{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stage)
		}
	}()
	root, err := os.OpenRoot(input.PackageRoot)
	if err != nil {
		return Generation{}, err
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
	}()
	source := root.FS()
	directories := map[string]bool{"": true}
	for _, file := range files {
		if !cleanPackagePath(file.Path) {
			return Generation{}, fmt.Errorf("packagegen: unsafe package path %q", file.Path)
		}
		if err := copyFile(source, stage, file); err != nil {
			return Generation{}, err
		}
		parent := path.Dir(file.Path)
		for parent != "." && parent != "" {
			directories[filepath.FromSlash(parent)] = true
			parent = path.Dir(parent)
		}
	}
	dirs := make([]string, 0, len(directories))
	for dir := range directories {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(os.PathSeparator)) > strings.Count(dirs[j], string(os.PathSeparator))
	})
	for _, dir := range dirs {
		path := stage
		if dir != "" {
			path = filepath.Join(stage, dir)
		}
		if err := syncDirectory(path); err != nil {
			return Generation{}, err
		}
	}
	if err := os.Rename(stage, generation.Path); err != nil {
		if info, statErr := os.Stat(generation.Path); statErr == nil && info.IsDir() {
			if verifyErr := verifyGeneration(generation.Path, input.PackageRoot, files); verifyErr == nil {
				return generation, nil
			}
		}
		return Generation{}, err
	}
	published = true
	if err := syncDirectory(packagesDir); err != nil {
		return Generation{}, err
	}
	if err := os.MkdirAll(generation.DataPath, 0o700); err != nil {
		return Generation{}, err
	}
	if err := requireDirectory(generation.DataPath); err != nil {
		return Generation{}, err
	}
	slog.Info("plugin package generation published", "generation_key", generation.Key, "target", input.Target, "files", len(files))
	return generation, nil
}

func Check(input Input) (Generation, error) {
	generation, err := Plan(input)
	if err != nil {
		return Generation{}, err
	}
	files, err := sortedFiles(input.Plugin)
	if err != nil {
		return Generation{}, err
	}
	info, err := os.Stat(generation.Path)
	if err != nil {
		return Generation{}, err
	}
	if !info.IsDir() {
		return Generation{}, fmt.Errorf("packagegen: generation %q is not a directory", generation.Key)
	}
	if err := verifyGeneration(generation.Path, input.PackageRoot, files); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func packageDigest(plugin *ir.Plugin) string {
	if plugin == nil {
		return ""
	}
	files, err := sortedFiles(plugin)
	if err != nil {
		return ""
	}
	h := sha256.New()
	for _, file := range files {
		writeIdentity(h, file.Path)
		writeIdentity(h, file.Digest)
		if file.Executable {
			writeIdentity(h, "x")
		} else {
			writeIdentity(h, "-")
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedFiles(plugin *ir.Plugin) ([]ir.PluginFile, error) {
	if plugin == nil {
		return nil, errors.New("packagegen: plugin must not be nil")
	}
	files := append([]ir.PluginFile(nil), plugin.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for i, file := range files {
		if !cleanPackagePath(file.Path) || file.Digest == "" {
			return nil, fmt.Errorf("packagegen: invalid package inventory path %q", file.Path)
		}
		if i > 0 && files[i-1].Path == file.Path {
			return nil, fmt.Errorf("packagegen: duplicate package inventory path %q", file.Path)
		}
	}
	return files, nil
}

func cleanPackagePath(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && path.Clean(name) == name && fs.ValidPath(name) && name != "."
}

func writeIdentity(dst io.Writer, value string) {
	_, _ = io.WriteString(dst, fmt.Sprintf("%d:", len(value)))
	_, _ = io.WriteString(dst, value)
}

func copyFile(source fs.FS, stage string, file ir.PluginFile) (retErr error) {
	data, sourceMode, code := hcc.ReadContainedSupportFile(source, file.Path, plugindecode.MaxPackageFileBytes)
	if code != "" {
		return fmt.Errorf("packagegen: %s: package path %q is unsafe", code, file.Path)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != file.Digest || sourceMode&0o111 != 0 != file.Executable {
		return fmt.Errorf("packagegen: source inventory changed for %q", file.Path)
	}
	dst := filepath.Join(stage, filepath.FromSlash(file.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if file.Executable {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func verifyGeneration(generation, sourceRoot string, files []ir.PluginFile) (retErr error) {
	sourceRootFile, err := os.OpenRoot(sourceRoot)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, sourceRootFile.Close())
	}()
	source := sourceRootFile.FS()
	generationRoot, err := os.OpenRoot(generation)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, generationRoot.Close())
	}()
	generated := generationRoot.FS()
	for _, file := range files {
		if !cleanPackagePath(file.Path) {
			return fmt.Errorf("packagegen: unsafe package path %q", file.Path)
		}
		data, targetMode, code := hcc.ReadContainedSupportFile(generated, file.Path, plugindecode.MaxPackageFileBytes)
		if code != "" {
			return fmt.Errorf("packagegen: immutable generation file %q is unsafe", file.Path)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.Digest {
			return fmt.Errorf("packagegen: immutable generation %q has changed", file.Path)
		}
		sourceData, sourceMode, code := hcc.ReadContainedSupportFile(source, file.Path, plugindecode.MaxPackageFileBytes)
		if code != "" {
			return fmt.Errorf("packagegen: source package file %q is unsafe", file.Path)
		}
		sourceSum := sha256.Sum256(sourceData)
		if hex.EncodeToString(sourceSum[:]) != file.Digest || (sourceMode&0o111 != 0) != file.Executable {
			return fmt.Errorf("packagegen: source inventory changed for %q", file.Path)
		}
		if (targetMode&0o111 != 0) != file.Executable {
			return fmt.Errorf("packagegen: immutable generation mode changed for %q", file.Path)
		}
	}
	return nil
}

func requireDirectory(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("packagegen: %q is not a directory", name)
	}
	return nil
}

func syncDirectory(name string) error {
	dir, err := os.Open(name)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
