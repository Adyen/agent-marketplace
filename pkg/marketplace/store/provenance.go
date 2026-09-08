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
