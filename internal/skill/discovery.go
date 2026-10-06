package skill

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// discoveredEntry represents a skill candidate found during directory walk.
// Directory-based skills have isFile=false and skillPath=directory containing SKILL.md.
// Standalone .md file skills have isFile=true and skillPath=the .md file itself.
type discoveredEntry struct {
	name         string // derived from directory name or filename (without .md)
	markdownPath string // path to the .md file to parse for frontmatter
	skillPath    string // path to use as Skill.Path (directory or file)
	isFile       bool   // true for standalone .md files, false for SKILL.md-in-directory
}

func DiscoverSkills(registryDir string) ([]Skill, []MalformedSkill, error) {
	if _, err := os.Stat(registryDir); err != nil {
		return nil, nil, fmt.Errorf("skill path %q does not exist in the registry cache (check the 'path' field in your config): %w", registryDir, err)
	}

	entries, err := discoverSkillEntries(registryDir)
	if err != nil {
		return nil, nil, err
	}

	slices.SortFunc(entries, func(a, b discoveredEntry) int {
		if cmp := strings.Compare(a.name, b.name); cmp != 0 {
			return cmp
		}
		if a.isFile != b.isFile {
			if a.isFile {
				return 1
			}
			return -1
		}
		return strings.Compare(a.markdownPath, b.markdownPath)
	})

	skills := make([]Skill, 0, len(entries))
	malformed := make([]MalformedSkill, 0)
	// Collision detection uses the parsed frontmatter name of valid skills only.
	// Standalone .md files without valid frontmatter are not real skills, so
	// duplicate basenames among non-skill documents (e.g. two different folders
	// each containing UseCases.md) must not block discovery.
	seen := make(map[string]string)
	for _, entry := range entries {
		skillName, description, tags, err := parseFrontmatterSource(registryDir, entry.markdownPath)
		if err != nil {
			// Standalone .md files without valid frontmatter are silently skipped
			// (e.g. README.md, CHANGELOG.md). Only SKILL.md directories report as malformed.
			if entry.isFile {
				continue
			}
			malformed = append(malformed, MalformedSkill{
				DirName: entry.name,
				Path:    entry.skillPath,
				Error:   err,
			})
			continue
		}

		// Standalone .md files with frontmatter that lacks a "name:" field are
		// not treated as skills (e.g. docs pages that only declare title/
		// description). Surfacing them with an empty Name produces nameless
		// rows in list output and meaningless entries in install flows, so we
		// silently drop them — matching the treatment of README.md and other
		// non-skill markdown documents.
		if entry.isFile && strings.TrimSpace(skillName) == "" {
			continue
		}

		// Prefer the parsed frontmatter name as the deduplication key. Fall back
		// to the directory/file basename when frontmatter omits it, so same-catalog
		// duplicates share the same effective name and the first deterministic
		// entry wins.
		key := skillName
		if key == "" {
			key = entry.name
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = entry.markdownPath

		skills = append(skills, Skill{
			Name:        skillName,
			Description: description,
			Tags:        tags,
			Path:        entry.skillPath,
		})
	}

	return skills, malformed, nil
}

var skippedDirectoryNames = map[string]struct{}{
	"node_modules": {},
	".git":         {},
	".cache":       {},
	"test":         {},
	"tests":        {},
	"__tests__":    {},
}

func discoverSkillEntries(registryDir string) ([]discoveredEntry, error) {
	var rawEntries []discoveredEntry
	skillDirs := make(map[string]bool) // tracks directories that contain SKILL.md

	if err := filepath.WalkDir(registryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}

		if !d.IsDir() {
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}

			if d.Name() == "SKILL.md" {
				skillDir := filepath.Dir(path)
				skillName := filepath.Base(skillDir)
				skillDirs[skillDir] = true
				rawEntries = append(rawEntries, discoveredEntry{
					name:         skillName,
					markdownPath: path,
					skillPath:    skillDir,
					isFile:       false,
				})
			} else {
				skillName := strings.TrimSuffix(d.Name(), ".md")
				rawEntries = append(rawEntries, discoveredEntry{
					name:         skillName,
					markdownPath: path,
					skillPath:    path,
					isFile:       true,
				})
			}
			return nil
		}

		if path == registryDir {
			return nil
		}

		name := d.Name()
		if shouldSkipDirectory(name, path, registryDir) {
			return filepath.SkipDir
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk registry dir %q: %w", registryDir, err)
	}

	// Filter out entries that live underneath another skill directory.
	// - Standalone .md files (e.g. skill-dir/references/notes.md) are
	//   auxiliary content belonging to the enclosing skill.
	// - Nested SKILL.md files (e.g. skill-dir/vendor/subskill/SKILL.md) are
	//   supplementary assets of the parent skill, not independent skills;
	//   they typically lack frontmatter and would otherwise surface as
	//   "malformed" in list output.
	// For SKILL.md entries we check strict ancestors (skip the entry's own
	// directory), so the legitimate top-level skill is kept.
	filtered := make([]discoveredEntry, 0, len(rawEntries))
	for _, entry := range rawEntries {
		start := filepath.Dir(entry.markdownPath)
		if !entry.isFile {
			parent := filepath.Dir(start)
			if parent == start {
				filtered = append(filtered, entry)
				continue
			}
			start = parent
		}
		if hasAncestorSkillDir(start, skillDirs) {
			continue
		}
		filtered = append(filtered, entry)
	}

	// Duplicate-name detection is deferred to DiscoverSkills, which checks the
	// parsed frontmatter name so non-skill markdown files (e.g. two README-like
	// docs that share a basename) don't incorrectly block discovery.
	return filtered, nil
}

func shouldSkipDirectory(name, path, root string) bool {
	if _, ignored := skippedDirectoryNames[name]; ignored {
		return true
	}

	if !strings.HasPrefix(name, ".") {
		return false
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}

	return !strings.HasPrefix(relativePath, "..") && relativePath != "."
}

// hasAncestorSkillDir returns true if dir or any of its ancestors is a
// directory containing a SKILL.md. Callers pass the directory from which the
// ancestor search should begin: for standalone .md files this is the file's
// parent directory; for SKILL.md files it is one level above the skill's own
// directory, so the top-level skill itself is not considered "nested".
func hasAncestorSkillDir(dir string, skillDirs map[string]bool) bool {
	for dir != "" && dir != "." {
		if skillDirs[dir] {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}
