package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
)

var behaviorRoots = []string{"cmd/", "internal/", "pkg/", "api/", "web/"}

const tuiJourneyDir = "tools/tui-qa/tests/"

var impactValue = regexp.MustCompile(`^none\s*(?:—|-)\s*(\S.*)$`)

type changedFile struct {
	path    string
	deleted bool
}

// gateRule requires that a pull request touching any bearing file also
// carries evidence, unless a commit has a "<trailer>: none — <reason>" trailer.
type gateRule struct {
	bearingLabel string
	evidence     string
	trailer      string
	missingKind  string
	reasonKind   string
	hint         string
	bearing      func(string) bool
	satisfies    func(changedFile) bool
}

var gateRules = []gateRule{
	{
		bearingLabel: "behavior-bearing",
		evidence:     "spec delta",
		trailer:      "Spec-Impact",
		missingKind:  kindMissingSpecDelta,
		reasonKind:   kindSpecImpactNoReason,
		bearing:      isBehaviorBearing,
		satisfies:    func(f changedFile) bool { return isSpecFile(f.path) },
	},
	{
		bearingLabel: "TUI-bearing",
		evidence:     "TUI QA journey",
		trailer:      "TUI-QA-Impact",
		missingKind:  kindMissingTUIQAJourney,
		reasonKind:   kindTUIQAImpactNoReason,
		hint:         "; add or extend a journey directly under " + tuiJourneyDir + " that drives the changed flow in the real terminal",
		bearing:      isTUIBearing,
		satisfies:    func(f changedFile) bool { return !f.deleted && isTUIJourney(f.path) },
	},
}

func isBehaviorBearing(p string) bool {
	if !hasAnyPrefix(p, behaviorRoots) || isExempt(p) {
		return false
	}
	return !strings.HasPrefix(p, "web/tests/")
}

func isTUIBearing(p string) bool {
	if isExempt(p) {
		return false
	}
	dir, name := path.Split(p)
	return strings.HasPrefix(p, "internal/tui/") ||
		(dir == "cmd/agent-marketplace/" && strings.HasPrefix(name, "tui") && strings.HasSuffix(name, ".go"))
}

func isExempt(p string) bool {
	return isTestFile(p) || strings.HasSuffix(strings.ToLower(p), ".md") || strings.Contains("/"+p, "/testdata/")
}

func isTUIJourney(p string) bool {
	dir, name := path.Split(p)
	return dir == tuiJourneyDir && (strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".test.ts"))
}

func isSpecFile(p string) bool {
	if !strings.HasSuffix(p, "/"+specFile) {
		return false
	}
	return strings.HasPrefix(p, specsDir+"/") ||
		(strings.HasPrefix(p, changesDir+"/") && strings.Contains(p, "/specs/"))
}

func hasAnyPrefix(p string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func (r gateRule) evaluate(files []changedFile, trailers []string) ([]finding, string) {
	var bearing []string
	satisfied := false
	for _, f := range files {
		satisfied = satisfied || r.satisfies(f)
		if r.bearing(f.path) {
			bearing = append(bearing, f.path)
		}
	}
	switch {
	case len(bearing) == 0:
		return nil, "no " + r.bearingLabel + " changes"
	case satisfied:
		return nil, fmt.Sprintf("%d %s files with %s", len(bearing), r.bearingLabel, r.evidence)
	}
	sawTrailer := false
	for _, t := range trailers {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		sawTrailer = true
		if m := impactValue.FindStringSubmatch(t); m != nil {
			return nil, r.trailer + ": none — " + m[1]
		}
	}
	if sawTrailer {
		return []finding{{r.reasonKind, fmt.Sprintf("use %q", r.trailer+": none — <reason>")}}, ""
	}
	return []finding{{r.missingKind, fmt.Sprintf(
		"%s files changed without a %s or %s trailer: %s%s",
		r.bearingLabel, r.evidence, r.trailer, strings.Join(bearing, ", "), r.hint)}}, ""
}

// evaluateGate decides every change-gate rule from the changed files and the
// values of each rule's trailer in the pull request range, keyed by trailer.
func evaluateGate(files []changedFile, trailers map[string][]string) ([]finding, string) {
	var (
		findings  []finding
		summaries []string
	)
	for _, r := range gateRules {
		f, s := r.evaluate(files, trailers[r.trailer])
		findings = append(findings, f...)
		if s != "" {
			summaries = append(summaries, s)
		}
	}
	return findings, strings.Join(summaries, "; ")
}

func gate(root, base, head string) ([]finding, string, error) {
	paths, err := diffPaths(root, base+"..."+head)
	if err != nil {
		return nil, "", err
	}
	deleted, err := diffPaths(root, base+"..."+head, "--diff-filter=D")
	if err != nil {
		return nil, "", err
	}
	isDeleted := map[string]bool{}
	for _, p := range deleted {
		isDeleted[p] = true
	}
	files := make([]changedFile, len(paths))
	for i, p := range paths {
		files[i] = changedFile{path: p, deleted: isDeleted[p]}
	}
	trailers := map[string][]string{}
	for _, r := range gateRules {
		logOut, err := git(root, "log", "--format=%(trailers:key="+r.trailer+",valueonly,unfold)", base+".."+head)
		if err != nil {
			return nil, "", err
		}
		trailers[r.trailer] = strings.Split(logOut, "\n")
	}
	findings, summary := evaluateGate(files, trailers)
	return findings, summary, nil
}

func diffPaths(root, rng string, extra ...string) ([]string, error) {
	out, err := git(root, slices.Concat([]string{"diff", "--no-renames", "--name-only", "-z"}, extra, []string{rng})...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
