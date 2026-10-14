package catalog

import (
	"errors"
	"io"
	"io/fs"
	"path"
)

var legacyNativeHookInventoryPaths = map[string]bool{
	".factory/hooks.json":   true,
	".cursor/hooks.json":    true,
	".devin/hooks.json":     true,
	".claude/settings.json": true,
}

func LegacyNativeHookInventoryPresent(fsys fs.FS) bool {
	for name := range legacyNativeHookInventoryPaths {
		if _, err := fs.Stat(fsys, name); err == nil {
			return true
		}
	}
	return false
}

type legacyNativeInventoryFS struct {
	fsys fs.FS
}

func (v legacyNativeInventoryFS) Open(name string) (fs.File, error) {
	if legacyNativeHookInventoryPaths[name] {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	file, err := v.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		return file, nil
	}
	return legacyNativeInventoryDir{File: file, dir: name}, nil
}

type legacyNativeInventoryDir struct {
	fs.File
	dir string
}

func (d legacyNativeInventoryDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rd, ok := d.File.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: d.dir, Err: errors.ErrUnsupported}
	}
	var out []fs.DirEntry
	for {
		want := n
		if n > 0 {
			want = n - len(out)
		}
		entries, err := rd.ReadDir(want)
		for _, entry := range entries {
			if !legacyNativeHookInventoryPaths[path.Join(d.dir, entry.Name())] {
				out = append(out, entry)
			}
		}
		if n <= 0 {
			return out, err
		}
		if err != nil || len(out) >= n || len(entries) == 0 {
			if errors.Is(err, io.EOF) && len(out) > 0 {
				return out, nil
			}
			return out, err
		}
	}
}
