package fsboundary

import (
	"errors"
	"os"
)

// Stable refusal identities shared by every descriptor-bound consumer.
// Callers branch on these with errors.Is, never on message text.
var (
	// ErrUnsafePath rejects a path-shaped violation: a relative or escaping
	// root or leaf, a symlink where policy forbids one, a dangling or cyclic
	// link, or a containment escape.
	ErrUnsafePath = errors.New("fsboundary: unsafe path")
	// ErrUnsafeFile rejects an object-shaped violation: a non-regular leaf
	// (directory, FIFO, socket, device) or a multiply linked regular file.
	ErrUnsafeFile = errors.New("fsboundary: unsafe file")
	// ErrTooLarge rejects content that exceeds its bounded read limit,
	// including growth observed while reading.
	ErrTooLarge = errors.New("fsboundary: file too large")
	// ErrGenerationConflict rejects an object whose captured identity no
	// longer matches, whether replaced, linked, or mutated in place.
	ErrGenerationConflict = errors.New("fsboundary: generation conflict")
)

// Identity pins one opened filesystem object. It is captured from the
// descriptor itself (fstat), so it describes the object the operation is
// bound to regardless of how the path changes afterwards.
type Identity struct {
	Device    uint64
	Inode     uint64
	Size      int64
	ModTimeNS int64
	Links     uint64
	Mode      os.FileMode
	UID       uint32
}

// SameObject reports whether both identities name the same filesystem
// object, ignoring size, timestamps, links, mode, and owner.
func (id Identity) SameObject(other Identity) bool {
	return id.Device == other.Device && id.Inode == other.Inode
}

// SameState reports whether both identities describe one unchanged object:
// same device and inode, and identical size, modification time, link count,
// mode, and owner. Any difference is a generation change.
func (id Identity) SameState(other Identity) bool {
	return id.SameObject(other) &&
		id.Size == other.Size &&
		id.ModTimeNS == other.ModTimeNS &&
		id.Links == other.Links &&
		id.Mode == other.Mode &&
		id.UID == other.UID
}

// LinkPolicy selects how [CaptureDestination] treats a symlink at the
// logical destination leaf.
type LinkPolicy int

const (
	// LinkPolicyUser is the user dotfile policy: a symlink leaf is honored
	// when its captured target is a regular file owned by the current user
	// with exactly one link. Reads and publications bind to the target; the
	// link itself is preserved and never replaced or followed for removal.
	LinkPolicyUser LinkPolicy = iota
	// LinkPolicyRepository is the repository/local policy: a symlink leaf is
	// honored only when its target, and every component on the way to it,
	// remains inside the captured canonical root.
	LinkPolicyRepository
)
