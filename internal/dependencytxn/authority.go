package dependencytxn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// Authority identifies the layer that owns a managed config/lock
// destination. Only the six layer-owned destinations carry authority:
// ~/.agents/{config,lock}.yaml, and <repo>/.agents/{config,lock}.yaml plus
// their .local.yaml personal variants. Every other path is legacy and keeps
// the pre-existing strict destination behavior.
type Authority int

const (
	// AuthorityLegacy marks paths outside the six managed destinations.
	AuthorityLegacy Authority = iota
	// AuthorityUser marks the user-personal layer below ~/.agents. Approved
	// dotfile links are honored: their target may live anywhere as long as
	// it is a regular file owned by the current user with exactly one link.
	AuthorityUser
	// AuthorityRepository marks a repository or personal-local layer below a
	// repository's .agents directory. Links are honored only while the
	// target, and every component on the way to it, stays inside the
	// captured repository root.
	AuthorityRepository
)

func (a Authority) String() string {
	switch a {
	case AuthorityUser:
		return "user"
	case AuthorityRepository:
		return "repository"
	default:
		return "legacy"
	}
}

// managedLeafNames are the only leaf names carrying layer authority.
var managedLeafNames = map[string]bool{
	"config.yaml":       true,
	"lock.yaml":         true,
	"config.local.yaml": true,
	"lock.local.yaml":   true,
}

// layerDirName is the single directory every managed destination lives in.
const layerDirName = ".agents"

// ClassifyDestination reports the layer authority for a path, lexically.
// The classification is positional: a managed leaf directly below an
// .agents directory, with the user layer recognized by comparing the
// canonical layer root against the canonical home directory.
func ClassifyDestination(path string) Authority {
	if !filepath.IsAbs(path) {
		return AuthorityLegacy
	}
	clean := filepath.Clean(path)
	if !managedLeafNames[filepath.Base(clean)] {
		return AuthorityLegacy
	}
	if filepath.Base(filepath.Dir(clean)) != layerDirName {
		return AuthorityLegacy
	}
	if home := canonicalHomeDir(); home != "" {
		if canonical, err := canonicalizeStatePath(filepath.Dir(filepath.Dir(clean))); err == nil && canonical == home {
			return AuthorityUser
		}
	}
	return AuthorityRepository
}

// canonicalHomeDir returns the canonical form of the current user's home
// directory, resolving through existing ancestors like StateDir does.
func canonicalHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	canonical, err := canonicalizeStatePath(home)
	if err != nil {
		return filepath.Clean(home)
	}
	return canonical
}

// canonicalManagedPath returns the stable transaction identity of a managed
// destination: the layer root canonicalized through existing ancestors, so
// the journal, lock sidecars, and dedupe all agree on one string for one
// layer file regardless of symlinked aliases above the layer root. The
// destination leaf itself is never resolved: an honored link stays named
// by its logical layer path.
func canonicalManagedPath(path string) (string, error) {
	if err := validateCleanAbsolutePath(path); err != nil {
		return "", err
	}
	if ClassifyDestination(path) == AuthorityLegacy {
		return "", fmt.Errorf("%w: not a managed config/lock destination: %s", ErrUnsafePath, path)
	}
	layerRoot := filepath.Dir(filepath.Dir(path))
	if canonical, err := canonicalizeStatePath(layerRoot); err == nil {
		layerRoot = canonical
	}
	return filepath.Join(layerRoot, layerDirName, filepath.Base(path)), nil
}

// validateManagedDestinationPath validates a managed destination's lexical
// identity and ancestors without capturing it: the layer root is
// canonicalized for a stable transaction identity, and existing ancestors
// are inspected with Lstat so a symlinked .agents (or any higher symlink)
// is refused instead of being canonicalized into another tree. The leaf
// itself is deliberately not inspected here; an honored link leaf is
// resolved by the descriptor boundary at capture, read, and publication
// time. Missing ancestors are tolerated so fresh repositories stage their
// first config the same way they always have.
func validateManagedDestinationPath(path string) (string, error) {
	canonical, err := canonicalManagedPath(path)
	if err != nil {
		return "", err
	}
	if err := validateDestinationAncestors(filepath.Dir(canonical)); err != nil {
		return "", err
	}
	return canonical, nil
}

// mapBoundaryErr folds the fsboundary refusal vocabulary into the
// dependency transaction vocabulary callers branch on today. Missing
// objects keep fs.ErrNotExist identity.
func mapBoundaryErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fsboundary.ErrUnsafePath):
		return fmt.Errorf("%w: %v", ErrUnsafePath, err)
	case errors.Is(err, fsboundary.ErrUnsafeFile):
		return fmt.Errorf("%w: %v", ErrUnsafePath, err)
	case errors.Is(err, fsboundary.ErrGenerationConflict):
		return fmt.Errorf("%w: %v", ErrGenerationConflict, err)
	case errors.Is(err, fsboundary.ErrTooLarge):
		return fmt.Errorf("%w: %v", ErrRecoveryBlocked, err)
	default:
		return err
	}
}

// danglingManagedConfigLinkError keeps the public dangling-layer condition
// even though the descriptor boundary cannot resolve the link target.
type danglingManagedConfigLinkError struct{ err error }

func (e *danglingManagedConfigLinkError) Error() string {
	return "resolve config path: symlink to a missing target: " + e.err.Error()
}
func (e *danglingManagedConfigLinkError) Unwrap() error { return e.err }

// mapManagedReadError keeps the established lock reader identities while
// retaining the boundary cause in the error chain.
func mapManagedReadError(path string, err error) error {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return err
	}
	mapped := mapBoundaryErr(err)
	if !isManagedLockBasename(filepath.Base(path)) {
		return mapped
	}
	switch {
	case errors.Is(mapped, ErrUnsafePath):
		return fmt.Errorf("%w: %w", lockfile.ErrUnsafeFile, mapped)
	case errors.Is(mapped, ErrRecoveryBlocked):
		return fmt.Errorf("%w: %w", lockfile.ErrFileTooLarge, mapped)
	default:
		return mapped
	}
}

func isManagedLockBasename(name string) bool {
	return name == "lock.yaml" || name == "lock.local.yaml"
}

func isManagedConfigBasename(name string) bool {
	return name == "config.yaml" || name == "config.local.yaml"
}

// boundDestination is one managed destination captured below its layer
// root: the root and destination descriptors pin the ancestor chain, an
// honored link's text, and the effective target identity from capture time
// until Close.
type boundDestination struct {
	path string
	root *fsboundary.Dir
	dest *fsboundary.Destination
}

func (b *boundDestination) Close() error {
	return errors.Join(b.dest.Close(), b.root.Close())
}

// bindDestination captures path as a managed destination. The layer's
// .agents directory must already exist; a missing one reports
// fs.ErrNotExist, like a missing destination parent always has.
func bindDestination(path string) (*boundDestination, error) {
	return bindManaged(path, false)
}

// bindDestinationForWrite captures path and creates the layer's .agents
// directory below the captured root first when it is missing, matching the
// directory creation the legacy writers perform.
func bindDestinationForWrite(path string) (*boundDestination, error) {
	return bindManaged(path, true)
}

func bindManaged(path string, createLayerDir bool) (*boundDestination, error) {
	canonical, err := canonicalManagedPath(path)
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(filepath.Dir(canonical)))
	if err != nil {
		return nil, mapBoundaryErr(err)
	}
	rel := filepath.Join(layerDirName, filepath.Base(canonical))
	policy := fsboundary.LinkPolicyRepository
	if ClassifyDestination(canonical) == AuthorityUser {
		policy = fsboundary.LinkPolicyUser
	}
	dest, err := fsboundary.CaptureDestination(root, rel, policy)
	if err != nil && createLayerDir && errors.Is(err, fs.ErrNotExist) {
		if mkdirErr := root.Mkdir(layerDirName, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
			_ = root.Close()
			return nil, mapBoundaryErr(mkdirErr)
		}
		dest, err = fsboundary.CaptureDestination(root, rel, policy)
	}
	if err != nil {
		_ = root.Close()
		return nil, mapBoundaryErr(err)
	}
	return &boundDestination{path: canonical, root: root, dest: dest}, nil
}

// inspect derives the destination's current generation and permission bits
// from the captured boundary: bytes and identity come from one verified
// read of the effective target.
func (b *boundDestination) inspect() (Generation, fs.FileMode, error) {
	if !b.dest.Exists() {
		return Generation{}, 0, nil
	}
	data, id, err := b.dest.ReadTarget(maxDependencyFileSize)
	if err != nil {
		return Generation{}, 0, mapBoundaryErr(err)
	}
	return Generation{Exists: true, Digest: Digest(data)}, id.Mode.Perm(), nil
}

// inspectManagedPath reads a managed destination's generation and mode
// through a fresh capture. A missing layer directory or destination reports
// a zero generation, exactly like the legacy inspector.
func inspectManagedPath(path string) (Generation, fs.FileMode, error) {
	bound, err := bindDestination(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Generation{}, 0, nil
	}
	if err != nil {
		return Generation{}, 0, err
	}
	defer func() { _ = bound.Close() }()
	return bound.inspect()
}

// readManagedSnapshot returns the destination bytes, generation, and mode
// through a fresh capture, honoring the layer's approved links.
func readManagedSnapshot(path string) ([]byte, Generation, fs.FileMode, error) {
	bound, err := bindDestination(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Generation{}, 0, nil
	}
	if err != nil {
		if isManagedConfigBasename(filepath.Base(path)) && errors.Is(err, ErrUnsafePath) {
			if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, Generation{}, 0, &danglingManagedConfigLinkError{err: err}
			}
		}
		return nil, Generation{}, 0, mapManagedReadError(path, err)
	}
	defer func() { _ = bound.Close() }()
	if !bound.dest.Exists() {
		return nil, Generation{}, 0, nil
	}
	data, id, err := bound.dest.ReadTarget(maxDependencyFileSize)
	if err != nil {
		return nil, Generation{}, 0, mapManagedReadError(path, err)
	}
	return data, Generation{Exists: true, Digest: Digest(data)}, id.Mode.Perm(), nil
}

// stageManaged writes the desired bytes to a fresh staged file below the
// captured layer directory. The parent chain is re-verified against the
// capture, so a replaced .agents cannot receive the staged file.
func stageManaged(bound *boundDestination, data []byte, mode fs.FileMode) (tempPath string, staging *fsboundary.Dir, name string, err error) {
	staging, err = bound.dest.StagingDir()
	if err != nil {
		return "", nil, "", mapBoundaryErr(err)
	}
	file, name, err := staging.CreateTemp(".dependencytxn-*.tmp")
	if err != nil {
		_ = staging.Close()
		return "", nil, "", fmt.Errorf("dependency transaction: create staged file: %w", err)
	}
	tempPath = filepath.Join(staging.Path(), name)
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = staging.Unlink(name)
			_ = staging.Close()
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", nil, "", fmt.Errorf("dependency transaction: set staged mode: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return "", nil, "", fmt.Errorf("dependency transaction: write staged file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", nil, "", fmt.Errorf("dependency transaction: sync staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", nil, "", fmt.Errorf("dependency transaction: close staged file: %w", err)
	}
	ok = true
	return tempPath, staging, name, nil
}

// rejectObjectAliases refuses a transaction in which two distinct layer
// paths resolve to the same filesystem object, whether through an honored
// link or a hard link. Such an alias would let one layer's write silently
// rewrite another layer's file, so the transaction fails before any
// capture, staging, journal, or destination mutation.
//
// Only managed layer participants are scanned, and each is identified by
// its logical layer path rather than by where its bytes happen to live:
// the config loader records read assertions at their resolved location, so
// an honored dotfile link legitimately appears once as its managed layer
// path and once as its resolved target. Legacy destinations keep their
// pre-existing semantics, where a read assertion may alias a published
// path and the generation guard arbitrates.
func rejectObjectAliases(files []DesiredFile, reads []ReadAssertion) error {
	seen := make(map[[2]uint64]string, len(files)+len(reads))
	check := func(identity, path string) error {
		info, err := os.Stat(path)
		if err != nil {
			// Missing or unreadable destinations are reported by capture.
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		key := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
		if previous, duplicate := seen[key]; duplicate && previous != identity {
			return fmt.Errorf("%w: layer destinations %s and %s resolve to the same object", ErrUnsafePath, previous, identity)
		}
		seen[key] = identity
		return nil
	}
	for _, file := range files {
		if ClassifyDestination(file.Path) == AuthorityLegacy {
			continue
		}
		if err := check(file.Path, file.Path); err != nil {
			return err
		}
	}
	for _, read := range reads {
		requested := read.RequestedPath
		if requested == "" {
			requested = read.Path
		}
		if ClassifyDestination(requested) == AuthorityLegacy {
			continue
		}
		identity, err := canonicalManagedPath(requested)
		if err != nil {
			identity = requested
		}
		if err := check(identity, read.Path); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDestinationDir creates the managed destination's layer directory
// when it is missing, descriptor-relatively below the captured layer root.
// Legacy destinations keep the caller's existing directory handling; this
// helper reports legacy paths as ErrUnsafePath so callers branch first.
func EnsureDestinationDir(path string) error {
	bound, err := bindDestinationForWrite(path)
	if err != nil {
		return err
	}
	return bound.Close()
}

// PublishDestination atomically replaces a managed destination's effective
// content with data at mode, preserving an approved layer link: the link
// itself is never detached, retargeted, or replaced; the captured target
// receives the new bytes. When expected.Exists is set the destination's
// current generation must match, a compare-and-swap precondition for
// callers that read before writing. Directory creation matches the legacy
// writers: a missing .agents is created below the captured layer root.
//
// The write is barrier-checked at capture, staging open, and publication:
// replacing the .agents parent or the destination leaf between those
// points fails with ErrGenerationConflict and removes the staged file
// without touching the replacement tree or any external bytes.
func PublishDestination(path string, expected Generation, data []byte, mode fs.FileMode, opts ...Option) error {
	if int64(len(data)) > maxDependencyFileSize {
		return fmt.Errorf("%w: destination %s exceeds %d bytes", ErrRecoveryBlocked, path, maxDependencyFileSize)
	}
	if mode != mode.Perm() {
		return fmt.Errorf("dependency transaction: invalid mode %o for %s", mode, path)
	}
	cfg := applyOptions(opts)
	bound, err := bindDestinationForWrite(path)
	if err != nil {
		return err
	}
	defer func() { _ = bound.Close() }()
	if expected.Exists {
		current, _, err := bound.inspect()
		if err != nil {
			return err
		}
		if current != expected {
			return fmt.Errorf("%w: %s", ErrGenerationConflict, bound.path)
		}
	}
	if err := inject(cfg, FaultPoint{Stage: FaultAfterCAS, Index: -1, Path: bound.path}); err != nil {
		return err
	}
	_, staging, name, err := stageManaged(bound, data, mode)
	if err != nil {
		return err
	}
	defer func() { _ = staging.Close() }()
	if err := inject(cfg, FaultPoint{Stage: FaultAfterStage, Index: -1, Path: bound.path}); err != nil {
		_ = staging.Unlink(name)
		return err
	}
	if err := cfg.syncDir(staging.Path()); err != nil {
		_ = staging.Unlink(name)
		return err
	}
	if err := inject(cfg, FaultPoint{Stage: FaultBeforePublish, Index: -1, Path: bound.path}); err != nil {
		_ = staging.Unlink(name)
		return err
	}
	if err := bound.dest.Publish(staging, name); err != nil {
		_ = staging.Unlink(name)
		return mapBoundaryErr(err)
	}
	return nil
}

// publishManagedJournal replays one journaled file through a fresh capture
// of its managed destination. It is the recovery counterpart of the
// commit-time bound publish: the destination state is classified from
// current bytes, and publication revalidates the journaled staged file
// before renaming descriptor-relatively.
func publishManagedJournal(file journalFile, syncDir func(string) error) error {
	state, err := classify(file)
	if err != nil {
		return err
	}
	if state == "new" {
		return nil
	}
	if state != "old" {
		return fmt.Errorf("%w: third-party bytes at %s", ErrRecoveryBlocked, file.Path)
	}
	bound, err := bindDestination(file.Path)
	if err != nil {
		return err
	}
	defer func() { _ = bound.Close() }()
	if file.Delete {
		if err := bound.dest.RemoveLeaf(); err != nil {
			return mapBoundaryErr(err)
		}
		return nil
	}
	if err := validateStaged(file); err != nil {
		return err
	}
	staging, err := bound.dest.StagingDir()
	if err != nil {
		return mapBoundaryErr(err)
	}
	defer func() { _ = staging.Close() }()
	if err := bound.dest.Publish(staging, filepath.Base(file.Temp)); err != nil {
		return mapBoundaryErr(err)
	}
	return nil
}
