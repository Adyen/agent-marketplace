package registryfs

// Policy is one registry tree's read policy: the root every read of that tree
// is confined to, plus whether the tree may contain symlinks at all.
//
// It is a value rather than a pair of arguments because the policy has to travel
// with a resource from discovery through resolution to materialization. Reading
// registry bytes happens in several layers and at several times: ingestion reads
// manifests, `sync` copies support files into the cache minutes later, the TUI
// renders a preview whenever the user presses a key. A tree validated once at
// load time says nothing about what those later reads will find, so each of them
// re-applies this policy at the point of use. Carrying a single value is what
// makes that hard to get wrong: a caller cannot forward the root and forget that
// the tree was remote.
type Policy struct {
	// Root is the absolute registry root that reads are confined to. Empty means
	// the caller cannot prove which registry a path came from; readers then
	// confine to the resource's own directory, which is narrower and never wider.
	Root string
	// RejectSymlinks refuses symlinks anywhere in the tree, contained or not. It
	// is set for remote registries: this tool produced the clone itself, so
	// nothing in the tree needs a link, and refusing keeps the cache byte-
	// comparable with the upstream commit.
	RejectSymlinks bool
}

// Remote returns the policy for a registry cloned from a remote URL.
func Remote(root string) Policy {
	return Policy{Root: root, RejectSymlinks: true}
}

// Local returns the policy for a registry the user pointed at on their own
// machine, where a symlink contained by the registry root is legitimate.
func Local(root string) Policy {
	return Policy{Root: root}
}

// At returns this policy rooted at root. It is how a nested tree — a sub-bundle,
// a canonicalized root, a cache directory — inherits the trust level of the tree
// it came from instead of silently degrading to "local".
func (p Policy) At(root string) Policy {
	return Policy{Root: root, RejectSymlinks: p.RejectSymlinks}
}

// FS opens a confined, read-only view of p.Root. The caller closes it.
func (p Policy) FS() (*FS, error) {
	return New(p.Root, p.RejectSymlinks)
}
