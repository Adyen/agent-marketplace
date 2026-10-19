package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// ErrInvalidWorkspace marks every Workspace validation failure: an empty
// repository root, a local-config root outside the repository, a zero
// Workspace passed to Resolve or Sync, or a root override that conflicts with
// the Workspace. Callers match it with errors.Is.
var ErrInvalidWorkspace = errors.New("invalid workspace")

// Workspace is the single validated root value shared by Resolve and Sync. It
// replaces the independently supplied RepoRoot, LocalConfigDir, and
// MaterializeRoot strings so the three roots cannot disagree.
//
// The repository root anchors the committed config layer and repo-relative
// catalog paths. The local-config root anchors the personal-local layer and
// local-entry relative paths; it must live inside the repository. The
// materialization root is derived from the local-config root — never supplied
// independently — so a nested-local workspace writes harness output beneath
// the nested directory while still loading the repository's committed config.
//
// A Workspace is immutable: construct it with NewWorkspace (explicit roots,
// used by tests and callers that already know both roots) or
// DiscoverWorkspace (production discovery from the working directory).
type Workspace struct {
	repoRoot       string
	localConfigDir string
}

// NewWorkspace constructs a Workspace from explicit roots. repoRoot is
// required; an empty localConfigDir derives a repository-root workspace. Both
// roots are normalized to absolute paths and the local-config root must be
// contained inside the repository root under the same symlink-aware policy
// the config loaders use.
func NewWorkspace(repoRoot, localConfigDir string) (Workspace, error) {
	absRepo, err := absoluteRoot(repoRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: repository root: %w", ErrInvalidWorkspace, err)
	}
	if localConfigDir == "" {
		localConfigDir = absRepo
	}
	absLocal, err := absoluteRoot(localConfigDir)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: local-config root: %w", ErrInvalidWorkspace, err)
	}
	if !workspacePathWithin(absRepo, absLocal) {
		return Workspace{}, fmt.Errorf("%w: local-config root %q is outside repository root %q", ErrInvalidWorkspace, absLocal, absRepo)
	}
	// Lexical containment alone is not enough: when the local-config root
	// does not exist yet beneath an in-repo symlink that points outside the
	// repository, the missing leaf keeps the unresolved path looking
	// contained while its physical parent escapes. Canonicalize both roots
	// through their deepest existing ancestors — the same policy
	// ownership.CanonicalRoot applies to not-yet-created target roots — and
	// require the physical paths to be contained too. The Workspace still
	// stores the logical absolute roots so valid not-yet-created nested
	// roots keep their user-visible spelling.
	canonicalLocal, err := ownership.CanonicalRoot(absLocal)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: local-config root: %w", ErrInvalidWorkspace, err)
	}
	canonicalRepo, err := ownership.CanonicalRoot(absRepo)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: repository root: %w", ErrInvalidWorkspace, err)
	}
	if !workspacePathWithin(canonicalRepo, canonicalLocal) {
		return Workspace{}, fmt.Errorf("%w: local-config root %q resolves outside repository root %q", ErrInvalidWorkspace, absLocal, absRepo)
	}
	return Workspace{repoRoot: absRepo, localConfigDir: absLocal}, nil
}

// DiscoverWorkspace constructs the production Workspace by discovering the
// repository root and local-config root from the current working directory,
// using the same upward-walk policy as the config loaders.
func DiscoverWorkspace() (Workspace, error) {
	return NewWorkspace(config.FindRepoRoot(), config.FindLocalConfigDir())
}

// workspaceForRoots builds the production Workspace for call sites whose
// options carry an explicit repository root and an optional local-config
// root. An empty localConfigDir preserves the previous LoadEffective
// fallback: the cwd-discovered local-config root applies when it lives
// inside the repository; otherwise the repository root derives every root,
// matching explicit test construction.
func workspaceForRoots(repoRoot, localConfigDir string) (Workspace, error) {
	if localConfigDir == "" {
		if discovered, err := absoluteRoot(config.FindLocalConfigDir()); err == nil {
			if repo, err := absoluteRoot(repoRoot); err == nil && workspacePathWithin(repo, discovered) {
				localConfigDir = discovered
			}
		}
	}
	return NewWorkspace(repoRoot, localConfigDir)
}

// WorkspaceForRoots is the exported form of workspaceForRoots for command
// packages that hold explicit roots and need the same validated Workspace the
// pipeline constructs internally.
func WorkspaceForRoots(repoRoot, localConfigDir string) (Workspace, error) {
	return workspaceForRoots(repoRoot, localConfigDir)
}

// RepoRoot returns the repository root: the committed config layer's base and
// the root used for catalog path resolution of repo-layer entries.
func (w Workspace) RepoRoot() string { return w.repoRoot }

// LocalConfigDir returns the local-config root: the personal-local layer's
// base and the directory local entries' relative paths resolve against.
func (w Workspace) LocalConfigDir() string { return w.localConfigDir }

// MaterializeRoot returns the root harness output is written beneath. It is
// derived from the local-config root so a nested-local workspace materializes
// below the nested directory, never independently of it.
func (w Workspace) MaterializeRoot() string { return w.localConfigDir }

// validate rejects the zero Workspace and rechecks physical containment before
// any loader, catalog access, backup, or materialization can use its logical
// roots. The filesystem can change after construction, including retargeting a
// symlink beneath the repository, so construction-time validation is not enough.
func (w Workspace) validate() error {
	if w.repoRoot == "" || w.localConfigDir == "" {
		return fmt.Errorf("%w: zero Workspace; construct one with NewWorkspace or DiscoverWorkspace", ErrInvalidWorkspace)
	}
	if _, err := NewWorkspace(w.repoRoot, w.localConfigDir); err != nil {
		return err
	}
	return nil
}

// checkEffectiveOptsRoots rejects an EffectiveOpts root override that
// conflicts with the Workspace. Alternate layer files and overlays stay
// available; a nonempty root override must name the same root the Workspace
// already validated, so the loader cannot read config from one root while
// materialization is derived from another.
func checkEffectiveOptsRoots(opts config.LoadEffectiveOptions, ws Workspace) error {
	if opts.RepoRoot != "" && !workspaceSamePath(opts.RepoRoot, ws.RepoRoot()) {
		return fmt.Errorf("%w: effective repository root %q conflicts with workspace repository root %q", ErrInvalidWorkspace, opts.RepoRoot, ws.RepoRoot())
	}
	if opts.LocalConfigDir != "" && !workspaceSamePath(opts.LocalConfigDir, ws.LocalConfigDir()) {
		return fmt.Errorf("%w: effective local-config root %q conflicts with workspace local-config root %q", ErrInvalidWorkspace, opts.LocalConfigDir, ws.LocalConfigDir())
	}
	return nil
}

// workspaceSamePath reports whether two paths name the same directory after
// absolute normalization and best-effort symlink resolution.
func workspaceSamePath(a, b string) bool {
	absA, err := absoluteRoot(a)
	if err != nil {
		return false
	}
	absB, err := absoluteRoot(b)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absA); err == nil {
		absA = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(absB); err == nil {
		absB = filepath.Clean(resolved)
	}
	return absA == absB
}

// absoluteRoot normalizes a root to a clean absolute path.
func absoluteRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// workspacePathWithin reports whether candidate is the same path as, or lives
// beneath, root, resolving symlinks on both sides first. This mirrors the
// containment policy config loaders apply when accepting discovered roots.
func workspacePathWithin(root, candidate string) bool {
	p := filepath.Clean(candidate)
	r := filepath.Clean(root)
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		p = filepath.Clean(rp)
	}
	if rr, err := filepath.EvalSymlinks(r); err == nil {
		r = filepath.Clean(rr)
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
