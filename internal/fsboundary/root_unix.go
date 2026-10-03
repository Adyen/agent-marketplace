//go:build unix

package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func (d *Dir) OpenRoot(rel string) (*Dir, error) {
	return d.openPath(rel, false, true, false, 0)
}

func (d *Dir) OpenContainedRoot(rel string) (*Dir, error) {
	return d.openPath(rel, false, true, true, 0)
}

func (d *Dir) OpenPath(rel string) (*Dir, error) {
	return d.openPath(rel, false, false, false, 0)
}

func (d *Dir) EnsureRoot(rel string) (*Dir, error) {
	return d.openPath(rel, true, true, false, 0)
}

func (d *Dir) EnsurePath(rel string) (*Dir, error) {
	return d.openPath(rel, true, false, false, 0)
}

func (d *Dir) openPath(rel string, create, rootLink, strictScope bool, hops int) (*Dir, error) {
	if hops > maxLinkHops {
		return nil, fmt.Errorf("%w: directory link chain exceeds %d hops", ErrUnsafePath, maxLinkHops)
	}
	var parts []string
	if rel != "." && rel != "" {
		var err error
		parts, err = splitRelative(rel)
		if err != nil {
			return nil, err
		}
	}
	current, err := openComponent(d.file, ".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if current != nil {
			_ = current.Close()
		}
	}()
	path := d.path
	for i, name := range parts {
		next, err := openComponent(current, name)
		if errors.Is(err, fs.ErrNotExist) && create {
			if mkdirErr := unix.Mkdirat(int(current.Fd()), name, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				return nil, fmt.Errorf("create output directory %s: %w", name, mkdirErr)
			}
			next, err = openComponent(current, name)
		}
		if err != nil && rootLink && i == len(parts)-1 && errors.Is(err, ErrUnsafePath) {
			var stat unix.Stat_t
			if statErr := unix.Fstatat(int(current.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); statErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFLNK {
				return nil, err
			}
			text, err := readLink(current, name)
			if err != nil {
				return nil, err
			}
			seen := false
			for _, component := range strings.Split(text, string(filepath.Separator)) {
				if component == ".." && seen {
					return nil, fmt.Errorf("%w: non-leading parent component in directory link", ErrUnsafePath)
				}
				seen = seen || component != "" && component != "." && component != ".."
			}
			candidate := text
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(path, text)
			}
			candidate = filepath.Clean(candidate)
			targetRel, err := filepath.Rel(d.path, candidate)
			if err != nil || targetRel == ".." || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) || filepath.IsAbs(targetRel) {
				if strictScope {
					return nil, fmt.Errorf("%w: directory link escapes its captured scope", ErrUnsafePath)
				}
				canonical, resolveErr := filepath.EvalSymlinks(candidate)
				if resolveErr != nil {
					return nil, fmt.Errorf("%w: resolve directory link: %w", ErrUnsafePath, resolveErr)
				}
				targetRel, err = filepath.Rel(d.path, canonical)
				if err != nil || targetRel == ".." || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) || filepath.IsAbs(targetRel) {
					return nil, fmt.Errorf("%w: directory link escapes its captured scope", ErrUnsafePath)
				}
			}
			target, err := d.openPath(targetRel, false, true, strictScope, hops+1)
			if err != nil {
				return nil, fmt.Errorf("%w: directory link target: %w", ErrUnsafePath, err)
			}
			return target, nil
		}
		if err != nil {
			return nil, err
		}
		_ = current.Close()
		current = next
		path = filepath.Join(path, name)
	}
	id, err := identityOf(current)
	if err != nil {
		return nil, err
	}
	result := &Dir{file: current, path: path, id: id}
	current = nil
	return result, nil
}
