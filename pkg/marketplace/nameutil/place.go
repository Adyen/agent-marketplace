package nameutil

import (
	"net/url"
	"regexp"
	"strings"
)

// TargetProfileKind is the virtual Marketplace kind for target profiles.
const TargetProfileKind = "target-profile"

var encodedPathSeparator = regexp.MustCompile("(?i)%(25)*(2f|5c)")

// IsTargetIdentifier reports whether value follows the place target identifier grammar.
func IsTargetIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// SplitPlaceKind separates a structured scoped kind into its target and local kind.
func SplitPlaceKind(kind string) (targetID, localKind string, ok bool) {
	if strings.Count(kind, "/") != 1 {
		return "", "", false
	}
	targetID, localKind, _ = strings.Cut(kind, "/")
	if !IsTargetIdentifier(targetID) || !IsTargetIdentifier(localKind) {
		return "", "", false
	}
	return targetID, localKind, true
}

// DecodePlaceKindToken accepts only a scoped kind's canonical escaped route token.
func DecodePlaceKindToken(raw string) (kind string, ok bool) {
	if strings.Count(raw, "%2F") != 1 || strings.Count(raw, "%") != 1 {
		return "", false
	}
	targetID, localKind, _ := strings.Cut(raw, "%2F")
	if !IsTargetIdentifier(targetID) || !IsTargetIdentifier(localKind) {
		return "", false
	}
	return targetID + "/" + localKind, true
}

// IsPlacePackageName reports whether name follows the place package name grammar.
func IsPlacePackageName(name string) bool {
	if len(name) == 0 || len(name) > 128 || !isPlacePackageNameCharacter(name[0], true) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isPlacePackageNameCharacter(name[i], false) {
			return false
		}
	}
	return true
}

// ContainsEncodedPathSeparator reports whether decoding raw reveals a slash or backslash.
func ContainsEncodedPathSeparator(raw string) bool {
	if encodedPathSeparator.MatchString(raw) {
		return true
	}
	decoded, err := url.PathUnescape(raw)
	return err == nil && encodedPathSeparator.MatchString(decoded)
}

func isPlacePackageNameCharacter(c byte, first bool) bool {
	if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
		return true
	}
	return !first && (c == '.' || c == '_' || c == '-')
}
