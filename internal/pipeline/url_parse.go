package pipeline

import (
	"net/url"
	pathpkg "path"
	"strings"
)

// ParsedWebURL holds the components extracted from a Git hosting web URL
// (GitLab, GitHub). If the URL doesn't match a known pattern, ok is false
// and the fields are empty.
type ParsedWebURL struct {
	GitURL       string // the git-cloneable URL (SSH or HTTPS)
	Ref          string // branch, tag, or commit SHA
	Path         string // subdirectory within the repo
	ResourceName string
	Ok           bool // true when the URL was successfully parsed
}

// ParseGitWebURL attempts to decompose a Git hosting web URL into its
// git-cloneable URL, ref, and path components. Supports:
//
//	GitLab: https://<host>/<group>/<project>/-/tree/<ref>[/<path>]
//	GitLab: https://<host>/<group>/<project>/-/blob/<ref>[/<path>]
//	GitHub: https://<host>/<group>/<project>/tree/<ref>[/<path>]
//	GitHub: https://<host>/<group>/<project>/blob/<ref>[/<path>]
//
// If the URL doesn't match any of these patterns, ok is false and the
// caller should treat it as a plain git URL (e.g. git@... or https://...git).
//
// For blob URLs (pointing to a single file), the path is set to the
// *directory* containing that file rather than the file itself, since
// the CLI operates on directories not files.
func ParseGitWebURL(rawURL string) ParsedWebURL {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ParsedWebURL{}
	}

	// If it's already a git SSH URL or ends with .git, no parsing needed.
	if strings.HasPrefix(rawURL, "git@") || strings.HasPrefix(rawURL, "ssh://") ||
		strings.HasPrefix(rawURL, "git://") || strings.HasSuffix(rawURL, ".git") {
		return ParsedWebURL{}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return ParsedWebURL{}
	}

	// Must have a scheme (https:// or http://) — bare host paths aren't web URLs.
	if u.Scheme != "https" && u.Scheme != "http" {
		return ParsedWebURL{}
	}

	host := u.Host
	path := strings.TrimPrefix(u.Path, "/")

	resourceName := hostedMarkdownResourceName(path)

	// Try GitLab pattern: <group>/<project>/-/tree/<ref>[/<path>]
	if gitURL, ref, subPath, ok := parseGitLabURL(host, path); ok {
		if resourceName != "" && strings.Contains(path, "/-/tree/") {
			subPath = parentPath(subPath)
		}
		return ParsedWebURL{GitURL: gitURL, Ref: ref, Path: subPath, ResourceName: resourceName, Ok: true}
	}

	// Try GitHub pattern: <group>/<project>/tree/<ref>[/<path>]
	if gitURL, ref, subPath, ok := parseGitHubURL(host, path); ok {
		if resourceName != "" && strings.Contains(path, "/tree/") {
			subPath = parentPath(subPath)
		}
		return ParsedWebURL{GitURL: gitURL, Ref: ref, Path: subPath, ResourceName: resourceName, Ok: true}
	}

	// Try bare project URL (no /-/tree/...): <group>/<project>
	if gitURL, ok := parseBareProjectURL(host, path); ok {
		return ParsedWebURL{GitURL: gitURL, Ok: true}
	}

	return ParsedWebURL{}
}

// parseGitLabURL matches the GitLab URL pattern:
// <group>/<project>/-/tree/<ref>[/<path>]
// <group>/<project>/-/blob/<ref>[/<path>]
// <group>/<project>/-/tree/<ref>/ (trailing slash, no path)
//
// GitLab also supports nested groups, so <group> can contain slashes.
// We look for /-/ as the separator between the project and the ref.
func parseGitLabURL(host, path string) (gitURL, ref, subPath string, ok bool) {
	idx := strings.Index(path, "/-/")
	if idx < 0 {
		return "", "", "", false
	}

	projectPart := path[:idx] // e.g. "example-org/infra-skills"
	after := path[idx+3:]     // e.g. "tree/main/skills/helm-chart-mirroring"

	// Split after into verb/ref/[path]
	parts := strings.SplitN(after, "/", 3)
	if len(parts) < 2 {
		return "", "", "", false
	}

	verb := parts[0]
	if verb != "tree" && verb != "blob" {
		return "", "", "", false
	}
	ref = parts[1]
	if ref == "" {
		return "", "", "", false
	}

	// For tree: path is the directory (or empty if just the root)
	// For blob: path is the directory *containing* the file
	if len(parts) == 3 && parts[2] != "" {
		rawPath := strings.TrimSuffix(parts[2], "/")
		if verb == "blob" {
			// Blob points to a file — use its parent directory
			lastSlash := strings.LastIndex(rawPath, "/")
			if lastSlash >= 0 {
				subPath = rawPath[:lastSlash]
			}
			// If the file is at the repo root (no slash), subPath stays ""
		} else {
			subPath = rawPath
		}
	}

	gitURL = buildGitURL(host, projectPart)
	return gitURL, ref, subPath, true
}

// parseGitHubURL matches the GitHub URL pattern:
// <group>/<project>/tree/<ref>[/<path>]
// <group>/<project>/blob/<ref>[/<path>]
func parseGitHubURL(host, path string) (gitURL, ref, subPath string, ok bool) {
	for _, verb := range []string{"tree", "blob"} {
		needle := "/" + verb + "/"
		idx := strings.Index(path, needle)
		if idx < 0 {
			continue
		}

		projectPart := path[:idx]
		after := path[idx+len(needle):]

		parts := strings.SplitN(after, "/", 2)
		ref = parts[0]
		if ref == "" {
			continue
		}

		if len(parts) == 2 && parts[1] != "" {
			rawPath := strings.TrimSuffix(parts[1], "/")
			if verb == "blob" {
				lastSlash := strings.LastIndex(rawPath, "/")
				if lastSlash >= 0 {
					subPath = rawPath[:lastSlash]
				}
			} else {
				subPath = rawPath
			}
		}

		gitURL = buildGitURL(host, projectPart)
		return gitURL, ref, subPath, true
	}

	return "", "", "", false
}

// knownSubpages are URL path suffixes used by GitHub/GitLab for non-repo
// pages. These must not be treated as part of a bare project path.
var knownSubpages = map[string]bool{
	"issues": true, "pulls": true, "pull": true, "actions": true,
	"settings": true, "wiki": true, "security": true, "projects": true,
	"milestones": true, "releases": true, "packages": true, "discussions": true,
	"commits": true, "branches": true, "tags": true, "compare": true,
	"network": true, "graphs": true, "stargazers": true, "forks": true,
	"watchers": true, "labels": true, "merge_requests": true, "pipelines": true,
}

func hostedMarkdownResourceName(path string) string {
	if strings.HasSuffix(path, "/") || (!strings.Contains(path, "/blob/") && !strings.Contains(path, "/tree/")) {
		return ""
	}
	base := pathpkg.Base(path)
	if strings.EqualFold(base, "SKILL.md") {
		return ""
	}
	ext := strings.ToLower(pathpkg.Ext(base))
	if ext != ".md" && ext != ".mdc" {
		return ""
	}
	return strings.TrimSuffix(base, pathpkg.Ext(base))
}

func parentPath(path string) string {
	parent := pathpkg.Dir(path)
	if parent == "." {
		return ""
	}
	return parent
}

// parseBareProjectURL handles URLs that point to a project root
// without any /tree/ or /-/tree/ path, e.g.:
// https://gitlab.example.com/example-org/infra-skills
// https://github.com/org/repo
func parseBareProjectURL(host, path string) (gitURL string, ok bool) {
	path = strings.TrimSuffix(path, "/")
	// Must have at least group/project
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", false
	}

	// Should not contain /-/ or /tree/ or /blob/ (those are handled above)
	if strings.Contains(path, "/-/") || strings.Contains(path, "/tree/") || strings.Contains(path, "/blob/") {
		return "", false
	}

	// Reject paths whose last segment is a known hosting subpage
	// (e.g. github.com/owner/repo/issues).
	if knownSubpages[parts[len(parts)-1]] {
		return "", false
	}

	return buildGitURL(host, path), true
}

// buildGitURL constructs an SSH git-cloneable URL from host and project path.
// SSH is preferred because internal GitLab instances typically require SSH
// auth and HTTPS prompts for credentials fail in non-interactive CLI mode.
func buildGitURL(host, projectPath string) string {
	return "git@" + host + ":" + projectPath
}

// ResolveFromURL takes a raw URL string and explicit --ref/--path flags,
// and returns the final (gitURL, ref, path) triple. Web URLs are parsed
// for embedded ref/path; explicit flags take precedence over parsed values.
func ResolveFromURL(rawURL, explicitRef, explicitPath string) (gitURL, ref, path string, ok bool) {
	parsed := ParseGitWebURL(rawURL)
	if !parsed.Ok {
		// Not a web URL — use as-is, with explicit flags
		return rawURL, explicitRef, explicitPath, true
	}

	gitURL = parsed.GitURL

	// Explicit flags win over URL-parsed values
	ref = parsed.Ref
	if explicitRef != "" {
		ref = explicitRef
	}

	path = parsed.Path
	if explicitPath != "" {
		path = explicitPath
	}

	return gitURL, ref, path, true
}
