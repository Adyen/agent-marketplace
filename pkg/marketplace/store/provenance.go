package store

import (
	"fmt"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
)

// NormalizeResourceOccurrence validates source provenance before it crosses
// the durable projection boundary. Empty provenance remains valid for legacy
// writers and rows.
func NormalizeResourceOccurrence(occ ResourceOccurrence) (ResourceOccurrence, error) {
	if occ.SourcePath != "" {
		normalized, err := NormalizeSourcePath(occ.SourcePath)
		if err != nil {
			return ResourceOccurrence{}, err
		}
		occ.SourcePath = normalized
	}
	return occ, nil
}

// NormalizeRegistryOccurrence removes unsafe origin components before durable
// storage. A malformed or non-public origin is represented as absent.
func NormalizeRegistryOccurrence(occ RegistryOccurrence) RegistryOccurrence {
	occ.SourceOrigin = publicurl.Sanitize(occ.SourceOrigin)
	return occ
}

// NormalizeSourcePath accepts only normalized repository-relative slash paths.
func NormalizeSourcePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("source path %q must be a repository-relative slash path", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return "", fmt.Errorf("source path %q must be normalized and repository-relative", value)
	}
	return clean, nil
}

// NormalizeRootPath normalizes a registry root path or a requirement path
// hint. It returns false for absolute, backslash, or escaping values; "."
// normalizes to the repository root "".
func NormalizeRootPath(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", false
	}
	clean := path.Clean(trimmed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	if clean == "." {
		return "", true
	}
	return clean, true
}

// DerivedSourceRoot is the single normalized registry occurrence root
// containing sourcePath: the normalized root is empty, equals sourcePath, or is
// a parent directory of it. The root is defined only when sourcePath is known
// and exactly one distinct normalized root qualifies.
func DerivedSourceRoot(roots []RegistryRoot, sourcePath string) (string, bool) {
	if sourcePath == "" {
		return "", false
	}
	derived := ""
	found := false
	for _, root := range roots {
		normalized, ok := NormalizeRootPath(root.Path)
		if !ok {
			continue
		}
		if normalized != "" && sourcePath != normalized && !strings.HasPrefix(sourcePath, normalized+"/") {
			continue
		}
		if found && derived != normalized {
			return "", false
		}
		derived = normalized
		found = true
	}
	return derived, found
}

// SourceRootMatches reports whether a requirement path hint equals the derived
// source root of a candidate. An empty hint always matches; an unsafe hint or
// an undefined derived root never matches.
func SourceRootMatches(roots []RegistryRoot, sourcePath, hint string) bool {
	trimmed := strings.TrimSpace(hint)
	if trimmed == "" {
		return true
	}
	normalized, ok := NormalizeRootPath(trimmed)
	if !ok {
		return false
	}
	derived, ok := DerivedSourceRoot(roots, sourcePath)
	return ok && derived == normalized
}
