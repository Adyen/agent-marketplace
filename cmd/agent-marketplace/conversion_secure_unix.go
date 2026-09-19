//go:build darwin || linux

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const conversionDirectoryMode = 0o755
const conversionDirectoryOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW

type conversionCreatedDirectory struct {
	parentFD int
	name     string
	path     string
}

func openConversionDirectoryForSnapshot(path string, create bool, snapshot *conversionRootSnapshot) (int, []conversionCreatedDirectory, error) {
	fd, created, err := openConversionDirectory(path, create)
	if err != nil || snapshot == nil {
		return fd, created, err
	}
	if snapshot.exists {
		duplicate, dupErr := unix.Dup(fd)
		if dupErr != nil {
			_ = unix.Close(fd)
			return -1, created, fmt.Errorf("duplicate conversion output root descriptor: %w", dupErr)
		}
		file := os.NewFile(uintptr(duplicate), path)
		if file == nil {
			_ = unix.Close(duplicate)
			_ = unix.Close(fd)
			return -1, created, errors.New("open conversion output root descriptor")
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil {
			_ = unix.Close(fd)
			return -1, created, fmt.Errorf("inspect opened conversion output root: %w", statErr)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(snapshot.info, info) {
			_ = unix.Close(fd)
			return -1, created, errors.New("conversion output root identity changed before publication")
		}
		return fd, created, nil
	}
	absolute, absErr := filepath.Abs(path)
	if absErr != nil {
		_ = unix.Close(fd)
		return -1, created, fmt.Errorf("resolve conversion output root %q: %w", path, absErr)
	}
	absolute = filepath.Clean(absolute)
	for _, directory := range created {
		if filepath.Clean(directory.path) == absolute {
			return fd, created, nil
		}
	}
	_ = unix.Close(fd)
	return -1, created, errors.New("conversion output root was created concurrently")
}

func openConversionDirectory(path string, create bool) (int, []conversionCreatedDirectory, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, nil, fmt.Errorf("resolve conversion directory %q: %w", path, err)
	}
	absolute = filepath.Clean(absolute)
	if absolute != string(filepath.Separator) {
		parent, parentErr := canonicalConversionPath(filepath.Dir(absolute))
		if parentErr != nil {
			return -1, nil, fmt.Errorf("resolve conversion directory parent %q: %w", filepath.Dir(absolute), parentErr)
		}
		absolute = filepath.Join(parent, filepath.Base(absolute))
	}
	fd, err := unix.Open(string(filepath.Separator), conversionDirectoryOpenFlags, 0)
	if err != nil {
		return -1, nil, fmt.Errorf("open conversion directory root: %w", err)
	}
	created := make([]conversionCreatedDirectory, 0)
	cleanupCreated := func() {
		closeConversionCreatedDirectories(created)
	}
	components := strings.Split(strings.TrimPrefix(absolute, string(filepath.Separator)), string(filepath.Separator))
	if len(components) == 1 && components[0] == "" {
		return fd, created, nil
	}
	currentPath := string(filepath.Separator)
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		next, openErr := unix.Openat(fd, component, conversionDirectoryOpenFlags, 0)
		createdNow := false
		if errors.Is(openErr, unix.ENOENT) && create {
			mkdirErr := unix.Mkdirat(fd, component, conversionDirectoryMode)
			if mkdirErr == nil {
				createdNow = true
			} else if !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(fd)
				cleanupCreated()
				return -1, nil, fmt.Errorf("create conversion directory %q: %w", filepath.Join(currentPath, component), mkdirErr)
			}
			next, openErr = unix.Openat(fd, component, conversionDirectoryOpenFlags, 0)
			if openErr == nil && createdNow {
				parentFD, dupErr := unix.Dup(fd)
				if dupErr != nil {
					_ = unix.Close(next)
					_ = unix.Close(fd)
					cleanupCreated()
					return -1, nil, fmt.Errorf("retain conversion directory parent: %w", dupErr)
				}
				created = append(created, conversionCreatedDirectory{parentFD: parentFD, name: component, path: filepath.Join(currentPath, component)})
			}
		}
		if openErr != nil {
			if createdNow {
				_ = unix.Unlinkat(fd, component, unix.AT_REMOVEDIR)
			}
			_ = unix.Close(fd)
			cleanupCreated()
			return -1, nil, fmt.Errorf("open conversion directory %q without following symlinks: %w", filepath.Join(currentPath, component), openErr)
		}
		_ = unix.Close(fd)
		fd = next
		currentPath = filepath.Join(currentPath, component)
	}
	return fd, created, nil
}

func ensureConversionDirectoryAt(rootFD int, rel, outDir string, created *[]conversionCreatedDirectory) (int, error) {
	rel = filepath.Clean(rel)
	if rel == "." {
		return unix.Dup(rootFD)
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return -1, fmt.Errorf("directory %q escapes output root %q", rel, outDir)
	}
	fd, err := unix.Dup(rootFD)
	if err != nil {
		return -1, fmt.Errorf("retain conversion output root: %w", err)
	}
	currentPath := filepath.Clean(outDir)
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		next, openErr := unix.Openat(fd, component, conversionDirectoryOpenFlags, 0)
		createdNow := false
		if errors.Is(openErr, unix.ENOENT) {
			mkdirErr := unix.Mkdirat(fd, component, conversionDirectoryMode)
			if mkdirErr == nil {
				createdNow = true
			} else if !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(fd)
				return -1, fmt.Errorf("create conversion destination directory %q: %w", filepath.Join(currentPath, component), mkdirErr)
			}
			next, openErr = unix.Openat(fd, component, conversionDirectoryOpenFlags, 0)
			if openErr == nil && createdNow {
				parentFD, dupErr := unix.Dup(fd)
				if dupErr != nil {
					_ = unix.Close(next)
					_ = unix.Close(fd)
					return -1, fmt.Errorf("retain conversion destination parent: %w", dupErr)
				}
				*created = append(*created, conversionCreatedDirectory{parentFD: parentFD, name: component, path: filepath.Join(currentPath, component)})
			}
		}
		if openErr != nil {
			if createdNow {
				_ = unix.Unlinkat(fd, component, unix.AT_REMOVEDIR)
			}
			_ = unix.Close(fd)
			return -1, fmt.Errorf("open conversion destination directory %q without following symlinks: %w", filepath.Join(currentPath, component), openErr)
		}
		_ = unix.Close(fd)
		fd = next
		currentPath = filepath.Join(currentPath, component)
	}
	return fd, nil
}

func createConversionRecoveryDirectory(outDir string) (path string, dirFD, parentFD int, name string, err error) {
	stageParent, err := codexConversionStageParent(outDir)
	if err != nil {
		return "", -1, -1, "", err
	}
	parentFD, _, err = openConversionDirectory(stageParent, false)
	if err != nil {
		return "", -1, -1, "", err
	}
	defer func() {
		if err != nil {
			_ = unix.Close(parentFD)
		}
	}()
	for attempt := 0; attempt < 100; attempt++ {
		var suffix [12]byte
		if _, err = rand.Read(suffix[:]); err != nil {
			return "", -1, -1, "", fmt.Errorf("generate conversion recovery name: %w", err)
		}
		name = ".agent-marketplace-recovery-" + hex.EncodeToString(suffix[:])
		if mkdirErr := unix.Mkdirat(parentFD, name, 0o700); errors.Is(mkdirErr, unix.EEXIST) {
			continue
		} else if mkdirErr != nil {
			return "", -1, -1, "", fmt.Errorf("create conversion recovery directory: %w", mkdirErr)
		}
		dirFD, err = unix.Openat(parentFD, name, conversionDirectoryOpenFlags, 0)
		if err != nil {
			_ = unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
			return "", -1, -1, "", fmt.Errorf("open conversion recovery directory: %w", err)
		}
		return filepath.Join(stageParent, name), dirFD, parentFD, name, nil
	}
	return "", -1, -1, "", errors.New("unable to create unique conversion recovery directory")
}

func conversionFileMatchesAt(dirFD int, name string, expectedInfo os.FileInfo, expectedDigest [sha256.Size]byte) (bool, error) {
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return false, errors.New("open conversion file descriptor")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if expectedInfo == nil || !info.Mode().IsRegular() || !os.SameFile(expectedInfo, info) {
		return false, nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest == expectedDigest, nil
}

func linkConversionFileAt(source string, destinationFD int, destinationName string) error {
	return unix.Linkat(unix.AT_FDCWD, source, destinationFD, destinationName, 0)
}

func linkConversionFileBetweenAt(sourceFD int, sourceName string, destinationFD int, destinationName string) error {
	return unix.Linkat(sourceFD, sourceName, destinationFD, destinationName, 0)
}

func renameConversionFileAt(sourceFD int, sourceName string, destinationFD int, destinationName string) error {
	return unix.Renameat(sourceFD, sourceName, destinationFD, destinationName)
}

func removeConversionFileAt(dirFD int, name string) error {
	return unix.Unlinkat(dirFD, name, 0)
}

func removeConversionDirectoryAt(dirFD int, name string) error {
	return unix.Unlinkat(dirFD, name, unix.AT_REMOVEDIR)
}

func closeConversionFD(fd *int) {
	if *fd >= 0 {
		_ = unix.Close(*fd)
		*fd = -1
	}
}

func closeConversionFDValue(fd int) {
	if fd >= 0 {
		_ = unix.Close(fd)
	}
}

func closeConversionCreatedDirectories(created []conversionCreatedDirectory) {
	for _, directory := range created {
		closeConversionFDValue(directory.parentFD)
	}
}

func closeConversionPublicationDirectories(publications []codexConversionPublication) {
	for _, publication := range publications {
		closeConversionFDValue(publication.destinationFD)
	}
}
