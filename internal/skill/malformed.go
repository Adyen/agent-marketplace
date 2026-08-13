package skill

// Correlating a selection with a dropped resource.
//
// An ingester that fails to load a resource appends an ingest.LoadFailure
// diagnostic and does NOT append the resource, so the resource never reaches
// catalog Entries. Every consumer that looks a selection up by name therefore
// sees an absence, and an absence is indistinguishable from an upstream
// deletion unless it is correlated with the malformed set first.
//
// This file is the single implementation of that correlation. Both the install
// path (pipeline.malformedSkillInstallError) and the sync path
// (resolver.SelectedMalformedError) call it, so the two cannot drift into
// disagreeing about whether a given selection is malformed or missing.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxFrontmatterLines bounds the best-effort frontmatter scan in
// declaredNameFromFrontmatter so an unterminated `---` fence in a large (or
// binary) file cannot turn name recovery into a full-file read.
const maxFrontmatterLines = 512

// resourceFileExtensions are the source-file extensions a resource can be
// authored in. They decide whether the last component of MalformedSkill.Path
// is a file (whose stem, or whose parent directory, names the resource) or a
// directory (which names the resource itself).
var resourceFileExtensions = map[string]struct{}{
	".md":   {},
	".json": {},
	".yaml": {},
	".yml":  {},
	".toml": {},
}

// MatchMalformed returns the malformed resource that the selection name refers
// to, if any, without constraining the resource kind. Prefer
// MatchMalformedForKind wherever the caller knows what kind it is looking for.
func MatchMalformed(malformed []MalformedSkill, name string) (MalformedSkill, bool) {
	return MatchMalformedForKind(malformed, "", name)
}

// MatchMalformedForKind returns the malformed resource of the given kind that
// the selection name refers to, if any. Matching is deliberately broader than
// an equality check on DirName: ingesters attach either the resource directory
// or the resource file to the diagnostic, and the authored `name:` can differ
// from both.
//
// kind is an ir.ResourceKind.String() label, or "" to accept any kind. A
// malformed entry that does not declare its own kind matches any kind, because
// a producer that cannot classify the failure must not silently suppress the
// correlation.
//
// It allocates nothing on the miss path: Resolve calls it once per
// (unresolved selection × malformed entry) pair, so materializing the candidate
// list per pair showed up as thousands of allocations on a documented hot path.
func MatchMalformedForKind(malformed []MalformedSkill, kind, name string) (MalformedSkill, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return MalformedSkill{}, false
	}
	for _, m := range malformed {
		if !kindsCompatible(m.Kind, kind) {
			continue
		}
		matched := false
		eachSelectionName(m, func(candidate string) bool {
			if candidate == name {
				matched = true
				return false
			}
			return true
		})
		if matched {
			return m, true
		}
	}
	return MalformedSkill{}, false
}

// kindsCompatible reports whether a malformed entry of kind entryKind can
// describe a selection of kind wantKind. An unknown kind on either side is
// permissive; two known and different kinds never correlate — a malformed skill
// directory named "foo" says nothing about a command or MCP server named "foo",
// and blaming it would name an unrelated file AND mask a stale selection that
// `sync --prune-stale` should have cleaned up.
func kindsCompatible(entryKind, wantKind string) bool {
	entryKind = strings.TrimSpace(entryKind)
	wantKind = strings.TrimSpace(wantKind)
	if entryKind == "" || wantKind == "" {
		return true
	}
	return entryKind == wantKind
}

// KindsCompatible reports whether a malformed entry of kind entryKind can
// describe a selection of kind wantKind. It exposes the package-local policy
// so other packages (e.g. the resolver's malformed index) do not duplicate it.
func KindsCompatible(entryKind, wantKind string) bool { return kindsCompatible(entryKind, wantKind) }

// MalformedSelectionNames returns every name a config selection could
// legitimately use to refer to a malformed resource:
//
//   - DirName, which resource-attached diagnostics set to the base of the
//     resource path (a skill directory, or a command/subagent file name);
//   - the directory component of Path, for ingesters that attach the SKILL.md
//     file rather than its directory;
//   - DeclaredName, the best-effort frontmatter `name:` — the only candidate
//     that covers a resource whose authored name differs from its directory.
//
// Bundle-level diagnostics (DirName is a diagnostic code, Path is the bundle
// root) describe no single resource and match nothing.
func MalformedSelectionNames(m MalformedSkill) []string {
	names := make([]string, 0, 4)
	eachSelectionName(m, func(candidate string) bool {
		names = append(names, candidate)
		return true
	})
	return names
}

// eachSelectionName yields the candidates of MalformedSelectionNames without
// allocating, stopping early when yield returns false. MalformedSelectionNames
// is the allocating convenience wrapper; matching uses this directly.
func eachSelectionName(m MalformedSkill, yield func(string) bool) {
	dirName := strings.TrimSpace(m.DirName)

	stop := false
	var seen [4]string
	count := 0
	add := func(candidate string) {
		if stop {
			return
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "." || candidate == "/" {
			return
		}
		for i := 0; i < count; i++ {
			if seen[i] == candidate {
				return
			}
		}
		if count < len(seen) {
			seen[count] = candidate
			count++
		}
		if !yield(candidate) {
			stop = true
		}
	}

	// A bundle whose ROOT is the resource is reported with DirName "."
	// (ingest.LoadFailure(..., "root skill", ".", ".", err)). The bundle root's
	// base names the cache checkout directory, not the resource, so the
	// recovered frontmatter name is the only candidate that may be trusted.
	if dirName == "." {
		add(m.DeclaredName)
		return
	}
	if looksLikeDiagnosticCode(dirName) {
		return
	}

	add(dirName)
	// A file-shaped DirName (e.g. "deploy.md" from a command load failure)
	// names the resource through its stem.
	add(trimResourceExtension(dirName))
	add(pathSelectionName(m.Path))
	add(m.DeclaredName)
}

// pathSelectionName derives the resource name from a malformed resource path.
// A skill-manifest file names its resource through its parent directory; any
// other file names it through its stem; a directory names it directly.
func pathSelectionName(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	if p == "" {
		return ""
	}
	p = strings.TrimSuffix(p, "/")
	base := path.Base(p)
	ext := strings.ToLower(path.Ext(base))
	if _, isFile := resourceFileExtensions[ext]; !isFile {
		return base
	}
	if strings.EqualFold(base, "SKILL.md") {
		return path.Base(path.Dir(p))
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

// trimResourceExtension strips a known resource source-file extension from
// name, returning "" when there is nothing to strip.
func trimResourceExtension(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if _, ok := resourceFileExtensions[ext]; !ok {
		return ""
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

// looksLikeDiagnosticCode reports whether a DirName is a diagnostic code
// (catalog.malformedFromDiagnostics uses the code as DirName for diagnostics
// that are not attached to a resource) rather than a resource name.
//
// A single dot is enough to decide: ir.ValidateName forbids '.' in a resource
// name, so once a source-file extension has been ruled out above, any remaining
// dot means a code. Requiring two would let the one-segment codes that exist
// today (diag.CodeJSONMalformed, diag.CodeJSONDuplicateKey, ownership.*) pass
// as resources, and a bundle-level diagnostic carries the BUNDLE path — whose
// last component would then become a selection candidate and could correlate a
// genuinely-absent selection with an unrelated failure.
func looksLikeDiagnosticCode(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\ \t") {
		return false
	}
	if _, isFile := resourceFileExtensions[strings.ToLower(path.Ext(name))]; isFile {
		return false
	}
	return strings.Contains(name, ".")
}

// MalformedSourceFile returns the file a user has to open to fix a malformed
// resource. Diagnostics attach either the resource directory or the resource
// file; a directory is completed with the skill manifest it must contain.
func MalformedSourceFile(m MalformedSkill) string {
	p := strings.TrimSpace(m.Path)
	if p == "" {
		return ""
	}
	p = strings.TrimSuffix(p, string(filepath.Separator))
	ext := strings.ToLower(filepath.Ext(p))
	if _, isFile := resourceFileExtensions[ext]; isFile {
		return p
	}
	return filepath.Join(p, "SKILL.md")
}

// ParseFailureForFile is the single wording of a validation failure, shared by
// the install diagnosis (pipeline.malformedSkillInstallError) and the sync
// diagnosis (resolver.SelectedMalformedError). It deliberately excludes the
// underlying error so callers can wrap it with %w and keep the error chain.
func ParseFailureForFile(file string) string {
	if file == "" {
		return "failed to parse"
	}
	return fmt.Sprintf("failed to parse %s", file)
}

// ParseFailure names the file a malformed resource failed to parse.
func ParseFailure(m MalformedSkill) string {
	return ParseFailureForFile(MalformedSourceFile(m))
}

// DeclaredNameFromFile reads the frontmatter `name:` value from a resource path
// on a best-effort basis, without requiring the YAML to be valid. It exists
// precisely for resources whose frontmatter does NOT parse: the declared name
// is usually still readable, and it is the only signal that can correlate a
// selection with a resource whose authored name differs from its directory.
//
// path may be a resource file or a resource directory (in which case its
// SKILL.md is read). Returns "" when nothing usable is found.
func DeclaredNameFromFile(path string) string {
	file := path
	if info, err := os.Stat(file); err == nil && info.IsDir() {
		file = filepath.Join(file, "SKILL.md")
	}
	// #nosec G304 -- file comes from an ingest diagnostic path inside the
	// local cache, not from untrusted input.
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	return declaredNameFromFrontmatter(f)
}

// declaredNameFromFrontmatter scans the leading `---` fenced block for a
// top-level `name:` key. It is a line scan, not a YAML parse, so a syntax error
// elsewhere in the block does not hide the name.
//
// The block must be CLOSED for the name to count. Without that requirement an
// unterminated opening fence turns the whole document body into "frontmatter",
// so any body line beginning with `name:` (a code sample, a docs snippet)
// becomes the recovered name — and that name can be a different, genuinely
// absent selection, which would blame this file for that selection's absence.
// A resource whose fence never closes cannot be identified from its content, so
// correlation falls back to the directory name.
func declaredNameFromFrontmatter(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)
	inFrontmatter := false
	candidate := ""
	// A frontmatter block that is never closed would otherwise scan the whole
	// file, and this runs once per malformed resource on every sync/status.
	// Real frontmatter is tens of lines; cap it well above that.
	for lines := 0; lines < maxFrontmatterLines && scanner.Scan(); lines++ {
		line := scanner.Text()
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "\ufeff"))
		if !inFrontmatter {
			if trimmed == "" {
				continue
			}
			if trimmed != "---" {
				return "" // no frontmatter block at all
			}
			inFrontmatter = true
			continue
		}
		if trimmed == "---" {
			// Closing fence: only now is the candidate trustworthy.
			return candidate
		}
		if candidate != "" {
			continue // first top-level name: wins
		}
		// Only a top-level key counts: an indented `name:` belongs to a nested
		// mapping (e.g. metadata) and does not name the resource.
		if line != trimmed {
			continue
		}
		rest, ok := strings.CutPrefix(trimmed, "name:")
		if !ok {
			continue
		}
		candidate = strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), `"'`))
	}
	return ""
}
