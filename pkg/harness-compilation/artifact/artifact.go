// Package artifact defines the output artifact type for compiled AI-agent
// configuration resources. An Artifact represents a single file to be written
// to disk, optionally as a fragment that must be merged with other fragments
// before materialization.
package artifact

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Artifact represents a single output file produced by compilation.
type Artifact struct {
	Path              string      // Relative path within the target install root
	Bytes             []byte      // File content
	Mode              fs.FileMode // File permissions (0 means use default)
	Fragment          bool        // True if this is a fragment to be merged
	FragmentKey       string      // Key for grouping fragments (empty if not a fragment)
	FragmentOrderKey  string      // Optional deterministic ordering key within a fragment group
	MergeWithExisting bool        // True if materialization merges into an existing file
	// ExternalReconciliationRequired marks shared files whose ownership-safe
	// patching belongs to the distribution layer rather than generic merging.
	ExternalReconciliationRequired bool
	ManagedByField                 string // Ownership marker field injected into managed entries
	ManagedByValue                 string // Ownership marker value injected into managed entries

	// DedupeFragmentEntries makes JSON fragment merging collapse semantically
	// identical entries instead of accumulating them, for the arrays of objects
	// that fragments contribute to (see MergeFragments).
	//
	// It is opt-in per fragment group so that no existing target changes
	// behavior. Devin hooks set it: the Devin plan requires that "Duplicate
	// desired hook tuples collapse before patching", and a Devin hook's
	// behavior identity is exactly the emitted rule object under its event key,
	// so semantic equality of those objects is the collapse rule.
	DedupeFragmentEntries bool
}

// Validate checks that the artifact's path is valid: relative, no ".." components,
// no absolute paths, and non-empty.
func (a *Artifact) Validate() error {
	return ValidatePath(a.Path)
}

// ValidatePath checks that a path is relative, contains no ".." components,
// is not absolute, and is non-empty.
func ValidatePath(path string) error {
	if path == "" {
		return errors.New("artifact path must not be empty")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("artifact path must be relative, got absolute: %s", path)
	}
	// Check for ".." in the original (uncleaned) path to reject any path
	// traversal attempt, even if filepath.Clean would resolve it safely.
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == ".." {
			return fmt.Errorf("artifact path must not contain '..': %s", path)
		}
	}
	return nil
}
