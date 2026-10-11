package ingest

import "io/fs"

func ReadSkillEntries(src fs.FS, root string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}
