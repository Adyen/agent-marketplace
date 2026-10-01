//go:build unix

package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// maxLinkHops bounds symlink chain resolution during capture. A chain
// longer than this is reported as a loop.
const maxLinkHops = 32

// maxLinkText bounds the raw text of a symlink accepted during capture.
const maxLinkText = 1 << 20

// CapturedLink records a logical destination leaf that was a symlink at
// capture time and satisfied the destination's link policy.
type CapturedLink struct {
	// ParentPath is the canonical path of the directory holding the link.
	ParentPath string
	// Name is the link's leaf name below ParentPath.
	Name string
	// TargetText is the raw link text captured at capture time. A changed
	// text is a generation conflict even when both texts resolve equally.
	TargetText string
	// Target is the canonical absolute path the link resolved to.
	Target string
}

// ancestor pins one verified path component below a captured root: the
// component's name and its identity captured at capture time.
type ancestor struct {
	name string
	id   Identity
}

type capturedTargetLink struct {
	root  *os.File
	chain []ancestor
	name  string
	text  string
}

// Destination is a captured publication or removal destination. Authority is
// bound to descriptors and identities captured from them: the logical leaf's
// ancestor chain, an honored link's text, and the effective target's
// identity. Renaming or replacing any component afterwards cannot redirect
// an operation; it is detected as a generation conflict instead.
//
// A Destination owns its descriptors. Close releases them. The root passed
// to CaptureDestination stays caller-owned and may be closed once capture
// returns.
type Destination struct {
	// logicalRoot is an independently owned duplicate of the capture root.
	logicalRoot *os.File
	// logicalChain pins each component from logicalRoot to the logical
	// leaf's parent.
	logicalChain []ancestor
	// logicalParentPath is the canonical path of the logical leaf's parent,
	// recorded at capture for diagnostics.
	logicalParentPath string
	// leafName is the logical leaf name.
	leafName string
	// isLink records whether the logical leaf was an honored symlink.
	isLink bool
	// linkText is the raw link text captured when isLink is true.
	linkText string

	targetLinks []capturedTargetLink

	// targetRoot is an independently owned descriptor of the effective
	// target's walk root: a duplicate of the capture root for targets inside
	// it, or "/" for user-policy targets outside it. Nil when the target
	// follows the logical chain.
	targetRoot *os.File
	// targetChain pins each component from targetRoot to the target's
	// parent. Nil when the target follows the logical chain.
	targetChain []ancestor
	// targetPath is the canonical absolute path of the effective leaf.
	targetPath string
	// targetName is the effective leaf name below the verified target
	// parent.
	targetName string
	// targetID is the effective target identity captured at capture time.
	targetID Identity
	// exists records whether the effective target existed at capture time.
	exists bool
}

// CaptureDestination captures rel below root as a publication or removal
// destination. Intermediate components must be real directories below the
// captured root; a symlinked ancestor is ErrUnsafePath.
//
// A direct leaf captures its own identity. A symlink leaf applies the link
// policy: LinkPolicyUser honors the link when its captured target is a
// regular file owned by the current user with exactly one link, wherever it
// lives; LinkPolicyRepository honors the link only when the target and every
// component on the way to it remain inside the captured canonical root.
// Dangling links, loops, escapes, and multiply linked or non-regular targets
// refuse with the stable errors. A missing leaf in an existing parent
// captures as an absent destination that Publish may create.
func CaptureDestination(root *Dir, rel string, policy LinkPolicy) (*Destination, error) {
	if policy != LinkPolicyUser && policy != LinkPolicyRepository {
		return nil, fmt.Errorf("fsboundary: unknown link policy %d", policy)
	}
	parts, err := splitRelative(rel)
	if err != nil {
		return nil, err
	}
	logicalRoot, err := openComponent(root.file, ".")
	if err != nil {
		return nil, fmt.Errorf("duplicate root %s: %w", root.path, err)
	}
	parentFd, chain, err := walkCapture(root.file, parts[:len(parts)-1])
	if err != nil {
		_ = logicalRoot.Close()
		return nil, err
	}
	defer func() { _ = parentFd.Close() }()

	dest := &Destination{
		logicalRoot:       logicalRoot,
		logicalChain:      chain,
		logicalParentPath: joinUnder(root.path, parts[:len(parts)-1]),
		leafName:          parts[len(parts)-1],
	}

	leaf, id, err := openLeaf(parentFd, dest.leafName)
	switch {
	case err == nil:
		_ = leaf.Close()
		dest.exists = true
		dest.targetID = id
		dest.targetName = dest.leafName
		dest.targetPath = filepath.Join(dest.logicalParentPath, dest.leafName)
	case errors.Is(err, errLeafSymlink):
		dest.isLink = true
		text, err := readLink(parentFd, dest.leafName)
		if err != nil {
			_ = dest.Close()
			return nil, err
		}
		dest.linkText = text
		if err := dest.captureTarget(root, policy); err != nil {
			_ = dest.Close()
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		// Absent leaf: publish may create it below the verified parent.
	default:
		_ = dest.Close()
		return nil, err
	}
	return dest, nil
}

// captureTarget resolves the honored link's effective target and pins its
// walk root, ancestor chain, and identity. The logical leaf's chain is
// already captured in dest.
func (d *Destination) captureTarget(root *Dir, policy LinkPolicy) error {
	text := d.linkText
	basePath := d.logicalParentPath
	for hops := 1; ; hops++ {
		if hops > maxLinkHops {
			return fmt.Errorf("%w: link chain over %d hops at %s", ErrUnsafePath, maxLinkHops, d.leafName)
		}
		candidate := text
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(basePath, text)
		}
		candidate = filepath.Clean(candidate)

		var targetRoot *os.File
		var chainParts []string
		var leafName string
		var canonical string
		switch policy {
		case LinkPolicyRepository:
			rel, err := containedRelative(root.path, candidate)
			if err != nil {
				return err
			}
			targetParts, err := splitRelative(rel)
			if err != nil {
				return err
			}
			targetRoot, err = openComponent(root.file, ".")
			if err != nil {
				return fmt.Errorf("duplicate root %s: %w", root.path, err)
			}
			chainParts = targetParts[:len(targetParts)-1]
			leafName = targetParts[len(targetParts)-1]
			canonical = filepath.Join(root.path, rel)
		case LinkPolicyUser:
			canonicalParent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
			if err != nil {
				return fmt.Errorf("%w: dangling or unresolvable link target %s", ErrUnsafePath, candidate)
			}
			targetRoot, err = openFilesystemRoot()
			if err != nil {
				return err
			}
			chainParts = splitAbsolute(canonicalParent)
			leafName = filepath.Base(candidate)
			canonical = filepath.Join(canonicalParent, leafName)
		}

		parentFd, chain, err := walkCapture(targetRoot, chainParts)
		if err != nil {
			_ = targetRoot.Close()
			return err
		}
		leaf, id, leafErr := openLeaf(parentFd, leafName)
		switch {
		case leafErr == nil:
			_ = leaf.Close()
			_ = parentFd.Close()
			if policy == LinkPolicyUser && id.UID != uint32(os.Geteuid()) {
				_ = targetRoot.Close()
				return fmt.Errorf("%w: link target owned by uid %d: %s", ErrUnsafeFile, id.UID, canonical)
			}
			d.targetRoot = targetRoot
			d.targetChain = chain
			d.targetPath = canonical
			d.targetName = leafName
			d.targetID = id
			d.exists = true
			return nil
		case errors.Is(leafErr, errLeafSymlink):
			// Chained link: continue from the link's own directory.
			nextText, err := readLink(parentFd, leafName)
			_ = parentFd.Close()
			if err != nil {
				_ = targetRoot.Close()
				return err
			}
			d.targetLinks = append(d.targetLinks, capturedTargetLink{root: targetRoot, chain: chain, name: leafName, text: nextText})
			text = nextText
			basePath = filepath.Dir(canonical)
			continue
		case errors.Is(leafErr, fs.ErrNotExist):
			_ = parentFd.Close()
			_ = targetRoot.Close()
			return fmt.Errorf("%w: dangling link target %s", ErrUnsafePath, candidate)
		default:
			_ = parentFd.Close()
			_ = targetRoot.Close()
			return leafErr
		}
	}
}

// Exists reports whether the effective target existed at capture time.
func (d *Destination) Exists() bool { return d.exists }

// Identity returns the effective target identity captured at capture time.
// It is the zero Identity when the destination did not exist.
func (d *Destination) Identity() Identity { return d.targetID }

// Path returns the canonical effective target path recorded at capture time.
// It is recorded for diagnostics; no operation resolves it again.
func (d *Destination) Path() string {
	if d.isLink {
		return d.targetPath
	}
	return filepath.Join(d.logicalParentPath, d.leafName)
}

// Link returns the honored link record, or nil for a direct destination.
func (d *Destination) Link() *CapturedLink {
	if !d.isLink {
		return nil
	}
	return &CapturedLink{
		ParentPath: d.logicalParentPath,
		Name:       d.leafName,
		TargetText: d.linkText,
		Target:     d.targetPath,
	}
}

// Close releases the captured descriptors.
func (d *Destination) Close() error {
	err := d.logicalRoot.Close()
	if d.targetRoot != nil {
		err = errors.Join(err, d.targetRoot.Close())
	}
	for _, link := range d.targetLinks {
		err = errors.Join(err, link.root.Close())
	}
	return err
}

// effectiveName returns the leaf name operations apply to: the target name
// for an honored link, the logical name otherwise.
func (d *Destination) effectiveName() string {
	if d.isLink {
		return d.targetName
	}
	return d.leafName
}

// verifyLogical re-walks the logical ancestor chain, requiring every
// component to still be the captured object, and returns an owned descriptor
// of the logical leaf's parent. A renamed, replaced, or reshaped component
// is a generation conflict.
func (d *Destination) verifyLogical() (*os.File, error) {
	return verifyChain(d.logicalRoot, d.logicalChain)
}

// verifyTarget verifies the logical chain, the honored link's text when the
// leaf is a link, and the target's ancestor chain, returning an owned
// descriptor of the effective target's parent.
func (d *Destination) verifyTarget() (*os.File, error) {
	logicalFd, err := d.verifyLogical()
	if err != nil {
		return nil, err
	}
	if !d.isLink {
		return logicalFd, nil
	}
	text, err := readLink(logicalFd, d.leafName)
	_ = logicalFd.Close()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: link %s removed since capture", ErrGenerationConflict, d.leafName)
	}
	if err != nil {
		return nil, err
	}
	if text != d.linkText {
		return nil, fmt.Errorf("%w: link %s changed since capture", ErrGenerationConflict, d.leafName)
	}
	for _, link := range d.targetLinks {
		parent, err := verifyChain(link.root, link.chain)
		if err != nil {
			return nil, err
		}
		current, err := readLink(parent, link.name)
		_ = parent.Close()
		if errors.Is(err, fs.ErrNotExist) || err == nil && current != link.text {
			return nil, fmt.Errorf("%w: intermediate link changed since capture", ErrGenerationConflict)
		}
		if err != nil {
			return nil, err
		}
	}
	return verifyChain(d.targetRoot, d.targetChain)
}

// verifyLeaf checks the effective leaf below parent against its captured
// state: an existing target must match its captured identity exactly, and an
// absent destination must still be absent. Any drift is a generation
// conflict.
func (d *Destination) verifyLeaf(parent *os.File) error {
	name := d.effectiveName()
	file, id, err := openLeaf(parent, name)
	if !d.exists {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err == nil:
			_ = file.Close()
			return fmt.Errorf("%w: destination appeared since capture: %s", ErrGenerationConflict, name)
		case errors.Is(err, errLeafSymlink), errors.Is(err, ErrUnsafeFile):
			return fmt.Errorf("%w: destination appeared since capture: %s", ErrGenerationConflict, name)
		default:
			return err
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: destination removed since capture: %s", ErrGenerationConflict, name)
	case errors.Is(err, errLeafSymlink), errors.Is(err, ErrUnsafeFile):
		return fmt.Errorf("%w: destination replaced since capture: %s", ErrGenerationConflict, name)
	case err != nil:
		return err
	}
	defer func() { _ = file.Close() }()
	if !id.SameState(d.targetID) {
		return fmt.Errorf("%w: destination changed since capture: %s", ErrGenerationConflict, name)
	}
	return nil
}

// Publish atomically installs the staged file at the captured destination.
// The staged file must have been fully written and synced by the caller;
// Publish revalidates its shape, verifies the destination against its
// captured state (ancestor chains, honored link text, and target identity),
// and then renames descriptor-relatively into the verified target parent,
// syncing the touched directories.
//
// On refusal nothing is removed: the destination's external bytes, modes,
// mtimes, and links are preserved exactly, and the staged file stays where
// the caller put it. Staging is caller-owned; this primitive only ever
// removes the staged name as part of a successful rename.
func (d *Destination) Publish(stagedParent *Dir, stagedName string) error {
	if err := validLeafName(stagedName); err != nil {
		return err
	}
	staged, _, err := openLeaf(stagedParent.file, stagedName)
	if err != nil {
		return fmt.Errorf("staged file: %w", err)
	}
	defer func() { _ = staged.Close() }()
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("sync staged file %s: %w", stagedName, err)
	}

	targetParent, err := d.verifyTarget()
	if err != nil {
		return err
	}
	defer func() { _ = targetParent.Close() }()
	if err := d.verifyLeaf(targetParent); err != nil {
		return err
	}

	if err := unix.Renameat(
		int(stagedParent.file.Fd()), stagedName,
		int(targetParent.Fd()), d.effectiveName(),
	); err != nil {
		return fmt.Errorf("publish %s: %w", d.Path(), err)
	}
	if err := fsyncFd(targetParent); err != nil {
		return err
	}
	return fsyncFd(stagedParent.file)
}

// RemoveLeaf removes the captured logical leaf. For an honored user or
// repository link it removes only the link itself, after verifying the link
// text and the captured target are unchanged; the target's bytes, mode, and
// mtime are preserved. For a direct leaf it verifies the captured identity
// and then unlinks descriptor-relatively below the verified parent. A leaf
// that was captured absent must still be absent; the removal is then a
// no-op. Any drift is a generation conflict that removes nothing.
func (d *Destination) RemoveLeaf() error {
	logicalFd, err := d.verifyLogical()
	if err != nil {
		return err
	}
	defer func() { _ = logicalFd.Close() }()

	if d.isLink {
		text, err := readLink(logicalFd, d.leafName)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: link %s removed since capture", ErrGenerationConflict, d.leafName)
		}
		if err != nil {
			return err
		}
		if text != d.linkText {
			return fmt.Errorf("%w: link %s changed since capture", ErrGenerationConflict, d.leafName)
		}
		targetParent, err := d.verifyTarget()
		if err != nil {
			return err
		}
		if err := d.verifyLeaf(targetParent); err != nil {
			_ = targetParent.Close()
			return err
		}
		_ = targetParent.Close()
		if err := unix.Unlinkat(int(logicalFd.Fd()), d.leafName, 0); err != nil {
			return fmt.Errorf("remove link %s: %w", d.leafName, err)
		}
		return fsyncFd(logicalFd)
	}

	if err := d.verifyLeaf(logicalFd); err != nil {
		return err
	}
	if !d.exists {
		return nil
	}
	if err := unix.Unlinkat(int(logicalFd.Fd()), d.leafName, 0); err != nil {
		return fmt.Errorf("remove %s: %w", d.leafName, err)
	}
	return fsyncFd(logicalFd)
}

// walkCapture opens each component of parts below start with no-follow
// directory flags, recording each component's identity, and returns an owned
// descriptor of the final directory with the ancestor chain. start stays
// caller-owned; when parts is empty the returned descriptor is an
// independent duplicate of start.
func walkCapture(start *os.File, parts []string) (*os.File, []ancestor, error) {
	current := start
	opened := false
	chain := make([]ancestor, 0, len(parts))
	for _, part := range parts {
		next, err := openComponent(current, part)
		if err != nil {
			if opened {
				_ = current.Close()
			}
			return nil, nil, err
		}
		id, err := identityOf(next)
		if err != nil {
			_ = next.Close()
			if opened {
				_ = current.Close()
			}
			return nil, nil, fmt.Errorf("inspect path component %s: %w", part, err)
		}
		chain = append(chain, ancestor{name: part, id: id})
		if opened {
			_ = current.Close()
		}
		current = next
		opened = true
	}
	if !opened {
		dup, err := openComponent(start, ".")
		if err != nil {
			return nil, nil, err
		}
		return dup, chain, nil
	}
	return current, chain, nil
}

// verifyChain re-walks chain below rootFd, requiring every component to
// still be the captured object, and returns an owned descriptor of the final
// directory. Renamed, removed, replaced, or reshaped components are
// generation conflicts. rootFd stays caller-owned.
func verifyChain(rootFd *os.File, chain []ancestor) (*os.File, error) {
	current := rootFd
	opened := false
	for _, a := range chain {
		next, err := openComponent(current, a.name)
		if err != nil {
			if opened {
				_ = current.Close()
			}
			switch {
			case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrUnsafePath):
				return nil, fmt.Errorf("%w: path component %s changed since capture", ErrGenerationConflict, a.name)
			default:
				return nil, err
			}
		}
		id, err := identityOf(next)
		if err != nil {
			_ = next.Close()
			if opened {
				_ = current.Close()
			}
			return nil, fmt.Errorf("inspect path component %s: %w", a.name, err)
		}
		if !id.SameObject(a.id) {
			_ = next.Close()
			if opened {
				_ = current.Close()
			}
			return nil, fmt.Errorf("%w: path component %s replaced since capture", ErrGenerationConflict, a.name)
		}
		if opened {
			_ = current.Close()
		}
		current = next
		opened = true
	}
	if !opened {
		dup, err := openComponent(rootFd, ".")
		if err != nil {
			return nil, err
		}
		return dup, nil
	}
	return current, nil
}

// readLink reads the raw text of the symlink name below parent. A leaf that
// stopped being a symlink since the no-follow open is a generation conflict.
func readLink(parent *os.File, name string) (string, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Readlinkat(int(parent.Fd()), name, buf)
		if err != nil {
			switch {
			case errors.Is(err, unix.EINVAL):
				return "", fmt.Errorf("%w: link %s replaced by a non-link during capture", ErrGenerationConflict, name)
			case errors.Is(err, unix.ENOENT):
				return "", fmt.Errorf("read link %s: %w", name, fs.ErrNotExist)
			default:
				return "", fmt.Errorf("read link %s: %w", name, err)
			}
		}
		if n < len(buf) {
			return string(buf[:n]), nil
		}
		if len(buf) >= maxLinkText {
			return "", fmt.Errorf("%w: link text over %d bytes: %s", ErrUnsafePath, maxLinkText, name)
		}
		buf = make([]byte, len(buf)*2)
	}
}

// containedRelative returns candidate's path relative to rootPath, or
// ErrUnsafePath when candidate escapes rootPath after normalization.
func containedRelative(rootPath, candidate string) (string, error) {
	rel, err := filepath.Rel(rootPath, candidate)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: link target escapes its captured root: %s", ErrUnsafePath, candidate)
	}
	return rel, nil
}

// splitAbsolute splits an absolute canonical path into its components below
// the filesystem root.
func splitAbsolute(path string) []string {
	trimmed := strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	if trimmed == "" || trimmed == "." {
		return nil
	}
	return strings.Split(trimmed, string(filepath.Separator))
}

// joinUnder joins components below base; with no components it returns base.
func joinUnder(base string, parts []string) string {
	if len(parts) == 0 {
		return base
	}
	elements := append([]string{base}, parts...)
	return filepath.Join(elements...)
}

// openFilesystemRoot opens "/" as a walk root for user-policy targets that
// live outside the captured root.
func openFilesystemRoot() (*os.File, error) {
	fd, err := unix.Open("/", dirOpenFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	file := os.NewFile(uintptr(fd), "/")
	return file, nil
}

// fsyncFd flushes a directory descriptor's metadata to stable storage.
func fsyncFd(file *os.File) error {
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", file.Name(), err)
	}
	return nil
}
