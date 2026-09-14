package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const mappedArchiveRoot = ".agent-marketplace-native/"
const devinRulesArchivePrefix = mappedArchiveRoot + "devin-rules/"

func existingMappedFiles(files map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for name, file := range files {
		if !strings.HasPrefix(name, mappedArchiveRoot) || filepath.Clean(name) != name || !filepath.IsAbs(file) {
			return nil, fmt.Errorf("backup: invalid mapped file")
		}
		if _, err := archiveTarget("/", name); err != nil {
			return nil, err
		}
		info, err := os.Lstat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("backup: mapped file is not regular")
		}
		result[name] = file
	}
	return result, nil
}

func existingMappedDirectories(directories map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for prefix, directory := range directories {
		if err := validateMappedPrefix(prefix); err != nil || !filepath.IsAbs(directory) {
			return nil, fmt.Errorf("backup: invalid mapped directory")
		}
		info, err := os.Lstat(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("backup: mapped directory is not a regular directory")
		}
		containmentRoot, err := mappedDirectoryContainmentRoot(directory)
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkAncestors(containmentRoot, directory); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(filepath.Join(directory, entry.Name()))
				if err != nil {
					return nil, err
				}
				if err := validateSymlinkTarget(link); err != nil {
					return nil, err
				}
			} else if !info.Mode().IsRegular() {
				continue
			}
			archivePath := prefix + entry.Name()
			if _, err := archiveTarget("/", archivePath); err != nil {
				return nil, err
			}
			result[archivePath] = filepath.Join(directory, entry.Name())
		}
	}
	return result, nil
}

func restoreMappedFile(reader io.Reader, header *tar.Header, mappings []map[string]string) error {
	destination := ""
	prefixDestination := false
	for _, mapping := range mappings {
		if file, ok := mapping[header.Name]; ok {
			if destination != "" {
				return fmt.Errorf("backup: ambiguous native mapping for %q", header.Name)
			}
			destination = file
		}
		for prefix, directory := range mapping {
			if err := validateMappedPrefix(prefix); err != nil || !strings.HasPrefix(header.Name, prefix) {
				continue
			}
			suffix := strings.TrimPrefix(header.Name, prefix)
			if suffix == "" || path.Base(suffix) != suffix || suffix == "." || suffix == ".." {
				return fmt.Errorf("backup: invalid mapped directory entry %q", header.Name)
			}
			if destination != "" {
				return fmt.Errorf("backup: ambiguous native mapping for %q", header.Name)
			}
			destination = filepath.Join(directory, suffix)
			prefixDestination = true
		}
	}
	if destination == "" || !filepath.IsAbs(destination) ||
		(header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeSymlink) {
		return fmt.Errorf("backup: unmapped native aggregate entry %q", header.Name)
	}
	if header.Typeflag == tar.TypeSymlink &&
		(!prefixDestination || !strings.HasPrefix(header.Name, devinRulesArchivePrefix)) {
		return fmt.Errorf("backup: symlink native mapping is not permitted for %q", header.Name)
	}
	parent := filepath.Dir(destination)
	if prefixDestination {
		containmentRoot, err := mappedDirectoryContainmentRoot(parent)
		if err != nil {
			return err
		}
		if err := rejectSymlinkAncestors(containmentRoot, parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("backup: unsafe native aggregate parent")
	}
	if prefixDestination && filepath.Clean(parent) == filepath.Clean(destination) {
		return fmt.Errorf("backup: unsafe mapped directory destination")
	}
	destinationInfo, destinationErr := os.Lstat(destination)
	if destinationErr != nil && !os.IsNotExist(destinationErr) {
		return destinationErr
	}
	if header.Typeflag == tar.TypeSymlink {
		if err := validateSymlinkTarget(header.Linkname); err != nil {
			return err
		}
		if destinationErr == nil && destinationInfo.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("backup: refusing to replace non-symlink native aggregate")
		}
		staged := filepath.Join(parent, ".agent-marketplace-restore-link-*")
		stagedFile, err := os.CreateTemp(parent, filepath.Base(staged))
		if err != nil {
			return err
		}
		staged = stagedFile.Name()
		if err := stagedFile.Close(); err != nil {
			_ = os.Remove(staged)
			return err
		}
		if err := os.Remove(staged); err != nil {
			return err
		}
		defer func() { _ = os.Remove(staged) }()
		if err := os.Symlink(header.Linkname, staged); err != nil {
			return err
		}
		return os.Rename(staged, destination)
	}
	if destinationErr == nil && !destinationInfo.Mode().IsRegular() {
		return fmt.Errorf("backup: refusing to replace non-regular native aggregate")
	}
	staged, err := os.CreateTemp(parent, ".agent-marketplace-restore-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	if _, err := io.Copy(staged, reader); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Chmod(header.FileInfo().Mode().Perm()); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), destination)
}

func validateMappedPrefix(prefix string) error {
	if prefix != devinRulesArchivePrefix {
		return fmt.Errorf("backup: invalid mapped archive prefix")
	}
	clean := path.Clean(strings.TrimSuffix(prefix, "/")) + "/"
	if clean != prefix || strings.Contains(strings.TrimPrefix(prefix, mappedArchiveRoot), "../") {
		return fmt.Errorf("backup: invalid mapped archive prefix")
	}
	if _, err := archiveTarget("/", strings.TrimSuffix(prefix, "/")); err != nil {
		return err
	}
	return nil
}

func mappedDirectoryContainmentRoot(directory string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("backup: home directory: %w", err)
	}
	rel, err := filepath.Rel(home, directory)
	if err != nil {
		return "", fmt.Errorf("backup: validate mapped directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("backup: mapped directory is outside the home directory")
	}
	return home, nil
}
