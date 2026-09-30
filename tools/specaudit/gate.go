package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var behaviorRoots = []string{"cmd/", "internal/", "pkg/", "api/", "web/"}

var specImpactValue = regexp.MustCompile(`^none\s*(?:—|-)\s*(\S.*)$`)

func isBehaviorBearing(p string) bool {
	if !hasAnyPrefix(p, behaviorRoots) || isTestFile(p) || strings.HasSuffix(strings.ToLower(p), ".md") {
		return false
	}
	return !strings.HasPrefix(p, "web/tests/") && !strings.Contains("/"+p, "/testdata/")
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

// evaluateGate decides the change gate from the changed paths and the values
// of every Spec-Impact trailer in the pull request range.
func evaluateGate(paths, trailers []string) ([]finding, string) {
	var behavior []string
	hasSpec := false
	for _, p := range paths {
		hasSpec = hasSpec || isSpecFile(p)
		if isBehaviorBearing(p) {
			behavior = append(behavior, p)
		}
	}
	switch {
	case len(behavior) == 0:
		return nil, "no behavior-bearing changes"
	case hasSpec:
		return nil, fmt.Sprintf("%d behavior-bearing files with spec delta", len(behavior))
	}
	sawTrailer := false
	for _, t := range trailers {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		sawTrailer = true
		if m := specImpactValue.FindStringSubmatch(t); m != nil {
			return nil, "Spec-Impact: none — " + m[1]
		}
	}
	if sawTrailer {
		return []finding{{kindSpecImpactNoReason, "use \"Spec-Impact: none — <reason>\""}}, ""
	}
	return []finding{{kindMissingSpecDelta, fmt.Sprintf(
		"behavior-bearing files changed without a spec delta or Spec-Impact trailer: %s",
		strings.Join(behavior, ", "))}}, ""
}

func gate(root, base, head string) ([]finding, string, error) {
	diff, err := git(root, "diff", "--no-renames", "--name-only", "-z", base+"..."+head)
	if err != nil {
		return nil, "", err
	}
	logOut, err := git(root, "log", "--format=%(trailers:key=Spec-Impact,valueonly,unfold)", base+".."+head)
	if err != nil {
		return nil, "", err
	}
	var paths []string
	for _, p := range strings.Split(diff, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	findings, summary := evaluateGate(paths, strings.Split(logOut, "\n"))
	return findings, summary, nil
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
