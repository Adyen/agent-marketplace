package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

const (
	releaseNoteHeading = "## Release note"
	breakingPrefix     = "Breaking:"
)

var (
	noneNote  = regexp.MustCompile(`^none(\s|—|-|$)`)
	listItem  = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)
	mergedPR  = regexp.MustCompile(`^Merge pull request #(\d+) from `)
	urlRemote = regexp.MustCompile(`^(?:https?|ssh)://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+)$`)
	scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):([^/].*)$`)
)

// releaseNote returns the lines of the first "## Release note" section outside
// fenced code, without trailing whitespace or leading and trailing blank lines,
// and whether the section exists at all.
func releaseNote(content string) ([]string, bool) {
	var (
		lines  []string
		found  bool
		fenced bool
	)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		} else if !fenced {
			if !found {
				found = strings.TrimSpace(line) == releaseNoteHeading
				continue
			}
			if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
				break
			}
		}
		if found {
			lines = append(lines, strings.TrimRight(line, " \t"))
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, found
}

// normalizedNote joins note lines into one whitespace-normalized line.
func normalizedNote(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func isNoneNote(text string) bool {
	return noneNote.MatchString(text)
}

// breakingText returns the text after the Breaking: prefix of a normalized note.
func breakingText(text string) (string, bool) {
	body, ok := strings.CutPrefix(text, breakingPrefix)
	return strings.TrimSpace(body), ok
}

// isArchivedProposal matches openspec/changes/archive/<dir>/proposal.md exactly
// one directory below the archive.
func isArchivedProposal(p string) bool {
	rest, ok := strings.CutPrefix(p, changesDir+"/"+archiveDir+"/")
	if !ok {
		return false
	}
	dir, file := path.Split(rest)
	return file == "proposal.md" && strings.Count(dir, "/") == 1
}

// releaseNotes renders the curated release notes of the archived proposals
// that the range adds. Breaking notes come first; none notes, empty breaking
// notes, and proposals without a release note section are omitted.
func releaseNotes(root, base, head, repoURL string) (string, error) {
	added, err := diffPaths(root, base+".."+head, "--diff-filter=A")
	if err != nil {
		return "", err
	}
	var breaking, changes []string
	for _, p := range added {
		if !isArchivedProposal(p) {
			continue
		}
		content, err := git(root, "show", head+":"+p)
		if err != nil {
			return "", err
		}
		lines, found := releaseNote(content)
		text := normalizedNote(lines)
		body, isBreaking := breakingText(text)
		if !found || text == "" || isNoneNote(text) || (isBreaking && body == "") {
			continue
		}
		pr, err := mergingPullRequest(root, base, head, p)
		if err != nil {
			return "", err
		}
		entry := strings.Join(entryLines(lines, isBreaking, pullRequestLink(pr, repoURL)), "\n")
		if isBreaking {
			breaking = append(breaking, entry)
		} else {
			changes = append(changes, entry)
		}
	}
	return renderReleaseNotes(breaking, changes), nil
}

// entryLines renders one note as a bullet. The first line holds the bullet
// text, the link ends the first paragraph, and later lines are indented under
// the bullet.
func entryLines(lines []string, breaking bool, link string) []string {
	if breaking {
		lines = breakingLines(lines)
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" && !listItem.MatchString(lines[end]) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
		end++
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case i == 0:
			out[i] = "- " + line
		case line == "":
			out[i] = ""
		default:
			out[i] = "  " + line
		}
	}
	out[end-1] += link
	return out
}

// breakingLines drops the Breaking: prefix from the first line. A first line
// left empty is removed along with the blank lines that follow it.
func breakingLines(lines []string) []string {
	first, _ := strings.CutPrefix(strings.TrimSpace(lines[0]), breakingPrefix)
	lines = append([]string{strings.TrimSpace(first)}, lines[1:]...)
	if lines[0] != "" {
		return lines
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

// mergingPullRequest returns the number of the oldest pull request merge in
// head that brought in the commit adding p, or "" when there is none.
func mergingPullRequest(root, base, head, p string) (string, error) {
	adds, err := git(root, "log", "--format=%H", "--diff-filter=A", "--no-renames", base+".."+head, "--", p)
	if err != nil {
		return "", err
	}
	lines := strings.Fields(adds)
	if len(lines) == 0 {
		return "", nil
	}
	commit := lines[len(lines)-1]
	merges, err := git(root, "log", "--ancestry-path", "--merges", "--reverse", "--format=%H%x00%s", commit+".."+head)
	if err != nil {
		return "", err
	}
	for _, record := range strings.Split(merges, "\n") {
		hash, subject, _ := strings.Cut(record, "\x00")
		m := mergedPR.FindStringSubmatch(subject)
		if m == nil {
			continue
		}
		brought, err := mergeBrings(root, commit, hash)
		if err != nil {
			return "", err
		}
		if brought {
			return m[1], nil
		}
	}
	return "", nil
}

// mergeBrings reports whether merge M brought commit C in through its second
// parent: C is an ancestor of M^2 and not of M^1.
func mergeBrings(root, commit, merge string) (bool, error) {
	fromBranch, err := isAncestor(root, commit, merge+"^2")
	if err != nil || !fromBranch {
		return false, err
	}
	fromBase, err := isAncestor(root, commit, merge+"^1")
	return !fromBase, err
}

// isAncestor reports whether a is an ancestor of b or equal to it.
func isAncestor(root, a, b string) (bool, error) {
	err := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", a, b).Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", a, b, err)
	}
}

func pullRequestLink(pr, repoURL string) string {
	switch {
	case pr == "":
		return ""
	case repoURL == "":
		return " (#" + pr + ")"
	default:
		return " ([#" + pr + "](" + repoURL + "/pull/" + pr + "))"
	}
}

func renderReleaseNotes(breaking, changes []string) string {
	var sections []string
	if len(breaking) > 0 {
		sections = append(sections, "## Breaking changes\n\n"+strings.Join(breaking, "\n")+"\n")
	}
	if len(changes) > 0 {
		sections = append(sections, "## Changes\n\n"+strings.Join(changes, "\n")+"\n")
	}
	return strings.Join(sections, "\n")
}

// releaseRepoURL returns the GitHub web URL of the repository, from the Actions
// environment when set and otherwise from the origin remote, or "" if neither
// yields one.
func releaseRepoURL(root string) string {
	server, repo := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY")
	if server != "" && repo != "" {
		return strings.TrimSuffix(server, "/") + "/" + repo
	}
	remote, err := git(root, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return remoteWebURL(strings.TrimSpace(remote))
}

// remoteWebURL converts a git remote (scp-like or URL form) to an https URL,
// dropping any credentials and a trailing ".git".
func remoteWebURL(remote string) string {
	var host, repoPath string
	if m := urlRemote.FindStringSubmatch(remote); m != nil {
		host, repoPath = m[1], m[2]
	} else if m := scpRemote.FindStringSubmatch(remote); m != nil {
		host, repoPath = m[1], m[2]
	} else {
		return ""
	}
	repoPath = strings.TrimSuffix(strings.TrimSuffix(repoPath, "/"), ".git")
	return fmt.Sprintf("https://%s/%s", host, repoPath)
}
