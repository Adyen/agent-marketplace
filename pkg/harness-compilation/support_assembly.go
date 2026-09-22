package harnesscompile

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// supportFileCandidate pairs the slash-separated source path (relative to the
// source fs root) with the slash-separated artifact path under the compiled
// target output root. strict marks a clean single-file reference: a value
// that consists of exactly the root token followed by one path, whose
// failure stays a hard error. Composite values (env assignments, PATH-style
// colon lists, multi-token commands) contribute best-effort candidates
// instead. Silent skipping is limited to clean candidates that are
// missing or do not resolve to regular bundle files; an unclean
// candidate (traversal, absolute, or backslash path) fails closed with
// bundle.support-file.unsafe regardless of strictness.
type supportFileCandidate struct {
	sourcePath string
	targetPath string
	strict     bool
}

// assembleResourceSupportArtifacts resolves the support-file declarations and
// explicitly referenced assets of one compiled resource against the source
// filesystem and returns the additional artifacts to publish.
//
// This is the only layer where both the source bytes (srcFS) and the compiled
// target layout are available: pure per-resource Compile is filesystem-free by
// contract, and IR carries SupportFiles as source-relative names only.
//
// Fail-closed contract: any missing, unsafe (traversal, symlink, non-regular),
// or unreadable support path produces an error diagnostic and NO support
// artifacts for that resource; the caller then suppresses the resource through
// the same per-resource rule that handles compiler error diagnostics. Target
// path collisions are left to the bundle-level duplicate-path rule.
func assembleResourceSupportArtifacts(srcFS fs.FS, resource ir.Resource, compiled []Artifact, t Target) ([]Artifact, []Diagnostic) {
	kind, name, err := ir.ValidateResource(resource)
	if err != nil || len(compiled) == 0 {
		// A resource that produced no artifacts at all owns no output, so no
		// support payload may be attached to it either.
		return nil, nil
	}
	var candidates []supportFileCandidate
	switch kind {
	case ir.KindSkill:
		skill, ok := resource.(*ir.Skill)
		if !ok || len(skill.SupportFiles) == 0 {
			return nil, nil
		}
		root, found := skillSupportTargetRoot(compiled)
		if !found {
			// No compiled skill root to anchor support files under; the
			// deferred-bytes diagnostic already reported for this resource
			// describes the outcome honestly.
			return nil, nil
		}
		sourceDir := skillSourceDir(skill)
		for _, rel := range skill.SupportFiles {
			candidates = append(candidates, supportFileCandidate{
				sourcePath: path.Join(sourceDir, rel),
				targetPath: root + "/" + rel,
				strict:     true,
			})
		}
	case ir.KindHook:
		hook, ok := resource.(*ir.Hook)
		if !ok {
			return nil, nil
		}
		for _, ref := range referencedAssetPaths(hook.Command) {
			candidates = append(candidates, supportFileCandidate{sourcePath: ref.rel, targetPath: ref.rel, strict: ref.strict})
		}
	case ir.KindMcpServer:
		server, ok := resource.(*ir.McpServer)
		if !ok {
			return nil, nil
		}
		values := make([]string, 0, len(server.Args)+len(server.Env)+len(server.Headers)+1)
		values = append(values, server.Command)
		values = append(values, server.Args...)
		for _, v := range server.Env {
			values = append(values, v)
		}
		for _, v := range server.Headers {
			values = append(values, v)
		}
		for _, ref := range referencedAssetPaths(values...) {
			candidates = append(candidates, supportFileCandidate{sourcePath: ref.rel, targetPath: ref.rel, strict: ref.strict})
		}
	default:
		return nil, nil
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	var diagnostics []Diagnostic
	var support []Artifact
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate.targetPath] {
			continue
		}
		seen[candidate.targetPath] = true
		if !isCleanRelativePath(candidate.targetPath) {
			// An unclean candidate is unsafe regardless of strictness: it
			// proves the published value can escape the bundle root at
			// runtime, so it fails closed before any read. Only
			// clean-but-missing or non-file best-effort candidates are
			// skipped silently below.
			diagnostics = append(diagnostics, supportFileDiagnostic(diag.CodeBundleSupportFileUnsafe, t, name, kind, candidate.sourcePath, "does not map to a clean target-relative path", resource))
			continue
		}
		data, mode, code, detail := readSupportFile(srcFS, candidate.sourcePath)
		if code != "" {
			if !candidate.strict {
				continue
			}
			diagnostics = append(diagnostics, supportFileDiagnostic(code, t, name, kind, candidate.sourcePath, detail, resource))
			continue
		}
		support = append(support, Artifact{Path: candidate.targetPath, Bytes: data, Mode: mode})
	}
	if len(diagnostics) > 0 {
		// Fail closed: a partial support set must never reach the target.
		return nil, diagnostics
	}
	return support, nil
}

// supportFileDiagnostic builds the stable error diagnostic for a refused
// support path. It carries only the slash-separated source-relative path,
// never file bytes or host filesystem paths.
func supportFileDiagnostic(code string, t Target, name string, kind ir.ResourceKind, rel, detail string, resource ir.Resource) Diagnostic {
	source := diag.SourceRef{}
	switch r := resource.(type) {
	case *ir.Skill:
		source = diag.SourceRef(r.Source)
	case *ir.Hook:
		source = diag.SourceRef(r.Source)
	case *ir.McpServer:
		source = diag.SourceRef(r.Source)
	}
	return Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Target:   string(t),
		Resource: name,
		Message:  fmt.Sprintf("%s %q support file %q %s", kind, name, rel, detail),
		Hint:     "Restore the file inside the bundle root or remove the reference to it",
		Source:   source,
	}
}

// skillSourceDir returns the directory of the skill's SKILL.md inside the
// source fs, or "." when the source reference is absent or names the root.
func skillSourceDir(skill *ir.Skill) string {
	if skill.Source.File == "" {
		return "."
	}
	return path.Dir(skill.Source.File)
}

// skillSupportTargetRoot locates the compiled resource root for a skill by
// finding the SKILL.md artifact the target emitted. Returning false means the
// target emitted no skill file to anchor support files under.
func skillSupportTargetRoot(compiled []Artifact) (string, bool) {
	for _, a := range compiled {
		if a.Fragment {
			continue
		}
		if strings.HasSuffix(a.Path, "/SKILL.md") {
			return strings.TrimSuffix(a.Path, "/SKILL.md"), true
		}
	}
	return "", false
}

// referencedAsset is one bundle-root-relative path discovered behind the
// canonical root token. strict marks a value that consists of exactly the
// token followed by one path — a deliberate single-file reference whose
// failure stays a hard error.
type referencedAsset struct {
	rel    string
	strict bool
}

// rootTokenPrefix is the canonical root token followed by the path separator
// that introduces a bundle-relative reference.
const rootTokenPrefix = bundle.CanonicalRootToken + "/"

// assetTerminators ends a referenced path inside a composite value. Colon is
// included so PATH-style tails such as "${CLAUDE_PLUGIN_ROOT}/bin:$PATH" do
// not fabricate a single missing path, and comma is included so
// comma-joined lists of URLs or references stay separable.
const assetTerminators = " \t\r\n'\"`);&|<>:,"

// referencedAssetPaths extracts bundle-root-relative paths that follow the
// canonical root token in the given values, e.g. the hooks/check.sh of
// "${CLAUDE_PLUGIN_ROOT}/hooks/check.sh". A value that is exactly one token
// reference yields a strict reference; composite values yield best-effort
// candidates, and tokens embedded inside a URL (scheme://…${TOKEN}/x) name
// remote URLs and are skipped entirely.
func referencedAssetPaths(values ...string) []referencedAsset {
	var refs []referencedAsset
	index := make(map[string]int)
	add := func(rel string, strict bool) {
		if rel == "" {
			return
		}
		if i, ok := index[rel]; ok {
			refs[i].strict = refs[i].strict || strict
			return
		}
		index[rel] = len(refs)
		refs = append(refs, referencedAsset{rel: rel, strict: strict})
	}
	for _, value := range values {
		// offeredRemainder records the full terminator-containing remainder
		// a single root-token value already offered as a best-effort
		// candidate. When set, the composite scan must not also publish a
		// terminator-truncated prefix of that same remainder: the remainder
		// is the better interpretation and the prefix was never
		// independently referenced.
		var offeredRemainder string
		// Strict single-token detection normalizes one matching
		// surrounding quote pair first: a quoted lone reference is still
		// one deliberate file contract, not a composite command. Quotes
		// also delimit the whole path, so whitespace inside them remains
		// part of the strict candidate rather than truncating it to a
		// prefix the author did not name.
		normalized, quoted := stripOneQuotePair(strings.TrimSpace(value))
		if strings.HasPrefix(normalized, rootTokenPrefix) {
			rel := normalized[len(rootTokenPrefix):]
			if quoted {
				add(rel, true)
				continue
			}
			if !strings.ContainsAny(rel, assetTerminators) {
				add(rel, true)
				continue
			} else if !strings.ContainsAny(rel, " \t\r\n") {
				// A whitespace-free remainder that contains declared
				// terminators may itself be a legal filename (for
				// example a comma-named file). Offer the full
				// remainder as a best-effort candidate so it can
				// resolve, while the composite scan below keeps
				// unresolvable expansions such as a $PATH tail
				// non-error.
				add(rel, false)
				offeredRemainder = rel
			}
		}
		// Composite scan: find each token occurrence by cursor so the URL
		// check can inspect the scheme text that precedes it. A token glued
		// inside a URL word (scheme://…${TOKEN}/x) names a remote URL and is
		// skipped, while a token joined to the URL word by a declared asset
		// terminator such as ':' or ',' starts a separate genuine bundle
		// reference. Occurrences in later words are still scanned either
		// way.
		start := 0
		for {
			idx := strings.Index(value[start:], rootTokenPrefix)
			if idx < 0 {
				break
			}
			idx += start
			if urlEmbeddedToken(value, idx) {
				// The token sits inside a URL word: a remote reference,
				// not a bundle asset. Skip only this occurrence.
				start = idx + len(rootTokenPrefix)
				continue
			}
			rest := value[idx+len(rootTokenPrefix):]
			end := strings.IndexAny(rest, assetTerminators)
			var rel string
			if end < 0 {
				rel, start = rest, len(value)
			} else {
				rel, start = rest[:end], idx+len(rootTokenPrefix)+end
			}
			if offeredRemainder != "" && strings.HasPrefix(offeredRemainder, rel) && len(offeredRemainder) > len(rel) {
				// rel is a terminator-truncated prefix of the full
				// remainder this single-token value already offered;
				// publishing it would invent a reference the author
				// never made.
				continue
			}
			add(rel, false)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].rel < refs[j].rel })
	return refs
}

// stripOneQuotePair removes exactly one matching surrounding quote pair
// (single, double, or backtick) from s and reports whether it did so.
// Anything else is returned unchanged.
func stripOneQuotePair(s string) (string, bool) {
	if len(s) >= 2 && s[0] == s[len(s)-1] &&
		(s[0] == '\'' || s[0] == '"' || s[0] == '`') {
		return s[1 : len(s)-1], true
	}
	return s, false
}

// urlEmbeddedToken reports whether the root-token occurrence at idx is glued
// inside a remote URL: the whitespace-delimited word shows a scheme
// separator ("://") or opens as a protocol-relative "//" word before the
// token, and the token is not separated from that URL text by a declared
// asset terminator. The second reference in "https://h/a,${TOKEN}/b" or
// "//h:${TOKEN}/c" is therefore a genuine bundle candidate, while
// "https://h/${TOKEN}/d" and "//h/${TOKEN}/e" stay remote.
func urlEmbeddedToken(value string, idx int) bool {
	wordStart := strings.LastIndexAny(value[:idx], " \t\r\n") + 1
	prefix := value[wordStart:idx]
	if !strings.HasPrefix(prefix, "//") && !strings.Contains(prefix, "://") {
		return false
	}
	return !strings.ContainsRune(assetTerminators, rune(value[idx-1]))
}

// readSupportFile validates rel against the source filesystem and reads its
// bytes and mode. It fails closed on traversal, absolute or unclean paths,
// symlink components, non-regular files, and read errors. The returned code
// is a stable diagnostic code; empty means success.
func readSupportFile(srcFS fs.FS, rel string) (data []byte, mode fs.FileMode, code string, detail string) {
	if !isCleanRelativePath(rel) {
		return nil, 0, diag.CodeBundleSupportFileUnsafe, "is not a clean bundle-relative path"
	}
	components := strings.Split(rel, "/")
	dir := "."
	for i, component := range components {
		entries, err := fs.ReadDir(srcFS, dir)
		if err != nil {
			return nil, 0, diag.CodeBundleSupportFileMissing, "could not be located in the source bundle"
		}
		var entry fs.DirEntry
		for _, e := range entries {
			if e.Name() == component {
				entry = e
				break
			}
		}
		if entry == nil {
			return nil, 0, diag.CodeBundleSupportFileMissing, "does not exist in the source bundle"
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil, 0, diag.CodeBundleSupportFileUnsafe, "traverses a symlink, which is refused"
		}
		if i < len(components)-1 {
			if !entry.IsDir() {
				return nil, 0, diag.CodeBundleSupportFileUnsafe, "traverses a non-directory component"
			}
			dir = path.Join(dir, component)
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil, 0, diag.CodeBundleSupportFileUnsafe, "is not a regular file"
		}
		data, err = fs.ReadFile(srcFS, rel)
		if err != nil {
			return nil, 0, diag.CodeBundleSupportFileMissing, "could not be read from the source bundle"
		}
		return data, info.Mode().Perm(), "", ""
	}
	return nil, 0, diag.CodeBundleSupportFileMissing, "does not exist in the source bundle"
}

// isCleanRelativePath reports whether rel is a slash-separated path that stays
// inside the bundle root: relative, non-empty, no backslashes, identical to its
// own clean form, and free of VCS/OS metadata components.
func isCleanRelativePath(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return false
	}
	if rel != path.Clean(rel) {
		return false
	}
	for _, component := range strings.Split(rel, "/") {
		if component == ".." || component == "." || bundle.IsSkillSupportMetadataName(component) {
			return false
		}
	}
	return true
}

// dropDiagnosticCode removes every diagnostic with the given code.
func dropDiagnosticCode(diagnostics []Diagnostic, code string) []Diagnostic {
	out := diagnostics[:0]
	for _, d := range diagnostics {
		if d.Code != code {
			out = append(out, d)
		}
	}
	return out
}
