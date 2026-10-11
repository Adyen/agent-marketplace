package backup

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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

func restoreMappedFile(reader io.Reader, header *tar.Header, mappings []map[string]string) (err error) {
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
	anchor, err := fsboundary.OpenDir(filepath.Dir(parent))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	captured, err := anchor.OpenPath(filepath.Base(parent))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := captured.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	validateParent := func() error {
		fresh, err := fsboundary.OpenDir(parent)
		if err != nil {
			return fmt.Errorf("%w: mapped parent %s no longer resolves: %w", fsboundary.ErrGenerationConflict, parent, err)
		}
		unchanged := fresh.Identity().SameObject(captured.Identity())
		if closeErr := fresh.Close(); closeErr != nil {
			err = closeErr
		}
		if err == nil && !unchanged {
			return fsboundary.ErrGenerationConflict
		}
		return err
	}
	return publishRestoredFile(captured, filepath.Base(destination), reader, header.FileInfo().Mode().Perm(), validateParent)
}
