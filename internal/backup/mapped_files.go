package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func existingMappedFiles(files map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for name, file := range files {
		if !strings.HasPrefix(name, ".agent-marketplace-native/") || filepath.Clean(name) != name || !filepath.IsAbs(file) {
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

func restoreMappedFile(reader io.Reader, header *tar.Header, mappings []map[string]string) error {
	destination := ""
	for _, mapping := range mappings {
		if file, ok := mapping[header.Name]; ok {
			destination = file
		}
	}
	if destination == "" || !filepath.IsAbs(destination) || header.Typeflag != tar.TypeReg {
		return fmt.Errorf("backup: unmapped native aggregate entry %q", header.Name)
	}
	parent := filepath.Dir(destination)
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("backup: unsafe native aggregate parent")
	}
	if info, err := os.Lstat(destination); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("backup: refusing to replace non-regular native aggregate")
	} else if err != nil && !os.IsNotExist(err) {
		return err
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
