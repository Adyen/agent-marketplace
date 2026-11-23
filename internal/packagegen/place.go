package packagegen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

// PlaceInput holds a verified profile binding and its package payload.
type PlaceInput struct {
	Binding targetprofile.Binding
	Files   []targetprofile.PackageFile
}

// PlanPlace verifies the binding and derives its immutable generation without writing.
func PlanPlace(input PlaceInput) (Generation, error) {
	binding, err := targetprofile.BuildBinding(input.Binding.ProfileIdentity, input.Files)
	if err != nil {
		return Generation{}, err
	}
	if binding != input.Binding {
		return Generation{}, errors.New("packagegen: place binding does not match its files")
	}
	stateRoot, err := StateRoot()
	if err != nil {
		return Generation{}, err
	}
	h := sha256.New()
	writeIdentity(h, "agent-marketplace/place-generation/v1")
	writeIdentity(h, input.Binding.ProfileIdentity.TargetID)
	writeIdentity(h, input.Binding.ProfileIdentity.SourceID)
	writeIdentity(h, input.Binding.ProfileIdentity.RevisionDigest)
	writeIdentity(h, input.Binding.PackageContentDigest)
	writeIdentity(h, input.Binding.DeliveryMappingDigest)
	key := hex.EncodeToString(h.Sum(nil))
	return Generation{
		Key:  key,
		Path: filepath.Join(stateRoot, "packages", key),
	}, nil
}

// PublishPlaceUnderPackagesLock publishes while the caller holds WithPackagesLock.
// Keep that lock through dependencytxn.CommitPlace or until a durable place intent records the generation.
func PublishPlaceUnderPackagesLock(input PlaceInput) (Generation, error) {
	files := make([]targetprofile.PackageFile, len(input.Files))
	for i, file := range input.Files {
		file.Bytes = append([]byte(nil), file.Bytes...)
		files[i] = file
	}
	input.Files = files
	generation, err := PlanPlace(input)
	if err != nil {
		return Generation{}, err
	}
	packagesDir := filepath.Dir(generation.Path)
	if err := requireDirectory(packagesDir); err != nil {
		return Generation{}, err
	}
	ordered := append([]targetprofile.PackageFile(nil), input.Files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].SourcePath < ordered[j].SourcePath })
	paths := make([]string, len(ordered))
	for i := range ordered {
		paths[i] = ordered[i].SourcePath
	}
	_, err = publishGeneration(generation, packagesDir, paths, func(stage string, index int) error {
		file := ordered[index]
		return writeGenerationFile(stage, file.SourcePath, file.Bytes, file.Mode)
	}, func(path string) error {
		return verifyPlaceGeneration(path, ordered)
	})
	if err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func verifyPlaceGeneration(generationPath string, files []targetprofile.PackageFile) (retErr error) {
	evidence, err := fsboundary.OpenDir(generationPath)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, evidence.Close())
	}()
	if evidence.Path() != generationPath {
		return fmt.Errorf("packagegen: place generation root is not its canonical spelling")
	}
	root, err := evidence.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
	}()

	expectedFiles := make(map[string]targetprofile.PackageFile, len(files))
	expectedDirectories := make(map[string]struct{})
	for _, file := range files {
		if !cleanPackagePath(file.SourcePath) {
			return fmt.Errorf("packagegen: unsafe place source path %q", file.SourcePath)
		}
		expectedFiles[file.SourcePath] = file
		parent := path.Dir(file.SourcePath)
		for parent != "." {
			expectedDirectories[parent] = struct{}{}
			parent = path.Dir(parent)
		}
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("packagegen: symlink in place generation at %q", name)
		}
		if entry.IsDir() {
			if _, ok := expectedDirectories[name]; !ok {
				return fmt.Errorf("packagegen: extra place generation directory %q", name)
			}
			delete(expectedDirectories, name)
			return nil
		}
		file, ok := expectedFiles[name]
		if !ok {
			return fmt.Errorf("packagegen: extra place generation file %q", name)
		}
		data, identity, err := evidence.ReadFile(filepath.FromSlash(name), int64(len(file.Bytes)))
		if err != nil {
			return fmt.Errorf("packagegen: read place generation file %q: %w", name, err)
		}
		mode := identity.Mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
		if !bytes.Equal(data, file.Bytes) || mode != file.Mode {
			return fmt.Errorf("packagegen: immutable place generation file %q changed", name)
		}
		delete(expectedFiles, name)
		return nil
	})
	if err != nil {
		return err
	}
	if len(expectedFiles) != 0 || len(expectedDirectories) != 0 {
		return fmt.Errorf("packagegen: immutable place generation is incomplete")
	}
	return nil
}
