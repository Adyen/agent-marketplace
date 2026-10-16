package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// exactGitLocalState is the repository metadata the exact-git checks need,
// read directly from files Git wrote in its canonical form.
type exactGitLocalState struct {
	gitDir       string
	configPath   string
	objectFormat string
	head         string
	worktreeRoot string
}

var exactGitConfigValuePattern = regexp.MustCompile(`^[a-z0-9]+$`)

var exactGitConfigAllowedKeys = map[string]map[string]bool{
	"core": {
		"repositoryformatversion": true,
		"filemode":                true,
		"bare":                    true,
		"logallrefupdates":        true,
		"ignorecase":              true,
		"precomposeunicode":       true,
	},
	"extensions": {
		"objectformat": true,
	},
}

// exactGitConfigKeyAllowed reports whether a lower-cased git config key such
// as "core.bare" is in the exact-git allowlist.
func exactGitConfigKeyAllowed(lower string) bool {
	section, key, found := strings.Cut(lower, ".")
	return found && exactGitConfigAllowedKeys[section][key]
}

// readExactGitLocalState returns the repository's object format, HEAD, and
// worktree root when every byte of its repository-local metadata matches the
// canonical form Git writes. It can only accept or decline: ok == false means
// "not sure" and callers must fall back to the subprocess checks, which remain
// the only path that can reject.
func readExactGitLocalState(repo string, bare bool) (exactGitLocalState, bool) {
	var state exactGitLocalState
	euid := uint32(os.Geteuid())
	dirOwnedByEUID := func(path string) bool {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		return ok && stat.Uid == euid
	}
	if !dirOwnedByEUID(repo) {
		return state, false
	}
	gitDir := repo
	if !bare {
		gitDir = filepath.Join(repo, ".git")
		if info, err := os.Lstat(gitDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return state, false
		}
		if _, err := os.Lstat(filepath.Join(gitDir, "commondir")); err == nil {
			return state, false
		}
	}
	if !dirOwnedByEUID(gitDir) {
		return state, false
	}
	configPath := filepath.Join(gitDir, "config")
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return state, false
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != euid {
		return state, false
	}
	if info.Size() <= 0 || info.Size() > 1<<20 {
		return state, false
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return state, false
	}
	objectFormat, configBare, ok := parseCanonicalExactGitConfig(raw)
	if !ok || configBare != strconv.FormatBool(bare) {
		return state, false
	}
	state = exactGitLocalState{gitDir: gitDir, configPath: configPath, objectFormat: objectFormat}
	if bare {
		return state, true
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return exactGitLocalState{}, false
	}
	wantLen := 40
	if objectFormat == "sha256" {
		wantLen = 64
	}
	if len(head) != wantLen+1 || head[wantLen] != '\n' {
		return exactGitLocalState{}, false
	}
	for _, c := range head[:wantLen] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return exactGitLocalState{}, false
		}
	}
	state.head = string(head[:wantLen])
	worktreeRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return exactGitLocalState{}, false
	}
	state.worktreeRoot = worktreeRoot
	return state, true
}

// parseCanonicalExactGitConfig accepts only the canonical form: [core] and
// [extensions] sections with tab-indented `key = value` lines, allowlisted
// lowercase keys, no duplicates, comments, blank lines, or CR bytes, and a
// trailing newline.
func parseCanonicalExactGitConfig(raw []byte) (objectFormat, coreBare string, ok bool) {
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || bytes.IndexByte(raw, '\r') >= 0 {
		return "", "", false
	}
	section := ""
	version := ""
	format := ""
	seen := make(map[string]bool)
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if line == "" {
			if i != len(lines)-1 {
				return "", "", false
			}
			continue
		}
		switch line {
		case "[core]", "[extensions]":
			section = line[1 : len(line)-1]
			continue
		}
		if !strings.HasPrefix(line, "\t") || section == "" {
			return "", "", false
		}
		key, value, found := strings.Cut(line[1:], " = ")
		if !found || !exactGitConfigAllowedKeys[section][key] || !exactGitConfigValuePattern.MatchString(value) {
			return "", "", false
		}
		full := section + "." + key
		if seen[full] {
			return "", "", false
		}
		seen[full] = true
		switch full {
		case "core.repositoryformatversion":
			version = value
		case "core.bare":
			coreBare = value
		case "extensions.objectformat":
			format = value
		}
	}
	switch {
	case format == "" && version == "0":
		return "sha1", coreBare, true
	case format == "sha256" && version == "1":
		return "sha256", coreBare, true
	}
	return "", "", false
}
