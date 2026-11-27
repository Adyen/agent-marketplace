package pipeline

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"golang.org/x/mod/semver"
)

const placeVersionProbeOutputLimit = 4 << 10

type boundedPlaceProbeOutput struct {
	data     []byte
	overflow bool
}

func (w *boundedPlaceProbeOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := placeVersionProbeOutputLimit - len(w.data)
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		w.data = append(w.data, data[:remaining]...)
	}
	if n > remaining {
		w.overflow = true
	}
	return n, nil
}

func probePlaceHostVersion(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profile targetprofile.Profile,
	home, dir string,
) (string, string, error) {
	probe := profile.Host.VersionProbe
	if probe == nil {
		return "", "unknown", nil
	}
	capture := &boundedPlaceProbeOutput{}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exitCode, err := runApprovedPlaceCommand(probeCtx, binding, probe.Argv, home, dir, capture)
	if ctx.Err() != nil {
		return "", "unknown", ctx.Err()
	}
	if err != nil || probeCtx.Err() != nil || exitCode != 0 || capture.overflow {
		return "", "unknown", nil
	}
	version, ok := parsePlaceHostVersion(capture.data, probe.Pattern)
	if !ok {
		return "", "unknown", nil
	}
	status := classifyPlaceHostVersion(version, profile.Host.Verified)
	if status == "unknown" {
		return "", status, nil
	}
	return version, status, nil
}

func parsePlaceHostVersion(output []byte, pattern string) (string, bool) {
	expression, err := regexp.Compile(pattern)
	if err != nil || expression.NumSubexp() > 1 {
		return "", false
	}
	matches := expression.FindAllSubmatch(output, -1)
	if len(matches) != 1 {
		return "", false
	}
	candidate := matches[0][0]
	if expression.NumSubexp() == 1 {
		if len(matches[0]) != 2 || len(matches[0][1]) == 0 {
			return "", false
		}
		candidate = matches[0][1]
	}
	version := string(candidate)
	if !targetprofile.IsCanonicalHostCoreVersion(version) {
		return "", false
	}
	return version, true
}

func classifyPlaceHostVersion(version string, verified []targetprofile.VerifiedRange) string {
	if !targetprofile.IsCanonicalHostCoreVersion(version) {
		return "unknown"
	}
	malformed := false
	for _, entry := range verified {
		matches, valid := placeVersionRangeMatches(version, entry.Range)
		if !valid {
			malformed = true
			continue
		}
		if matches {
			return "verified"
		}
	}
	if malformed {
		return "unknown"
	}
	return "outside-verified-range"
}

func placeVersionRangeMatches(version, versionRange string) (bool, bool) {
	fields := strings.Fields(versionRange)
	if len(fields) == 0 {
		return false, false
	}
	if len(fields) == 1 && targetprofile.IsCanonicalHostCoreVersion(fields[0]) {
		return version == fields[0], true
	}
	allMatch := true
	for _, field := range fields {
		operator, bound := "", field
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(field, candidate) {
				operator, bound = candidate, strings.TrimPrefix(field, candidate)
				break
			}
		}
		if operator == "" || !targetprofile.IsCanonicalHostCoreVersion(bound) {
			return false, false
		}
		comparison := semver.Compare("v"+version, "v"+bound)
		matches := false
		switch operator {
		case ">=":
			matches = comparison >= 0
		case "<=":
			matches = comparison <= 0
		case ">":
			matches = comparison > 0
		case "<":
			matches = comparison < 0
		case "=":
			matches = comparison == 0
		}
		if !matches {
			allMatch = false
		}
	}
	return allMatch, true
}
