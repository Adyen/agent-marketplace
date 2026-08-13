package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// stateDirName is the Agent Marketplace subdirectory of the XDG state root.
	stateDirName = "agent-marketplace"
	// ownershipDirName holds one directory per canonical target root.
	ownershipDirName = "ownership"
	// safetyDirName holds mandatory raw-byte safety backups.
	safetyDirName = "safety-backups"

	stateFileName   = "state.json"
	journalFileName = "journal.json"

	// keyDomain domain-separates the root-key hash so a digest computed here can
	// never be confused with any other digest in the product.
	keyDomain = "agent-marketplace/ownership/root/v1"
)

// StateRoot returns $XDG_STATE_HOME/agent-marketplace, defaulting to
// ~/.local/state/agent-marketplace when XDG_STATE_HOME is unset or is not an
// absolute path (the XDG spec says a relative value must be ignored).
//
// The plan pins both spellings verbatim under "Durable state".
func StateRoot() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, stateDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ownership: home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", stateDirName), nil
}

// CanonicalRoot normalizes a target root into the single spelling that keys its
// ownership record. The steps, and why each one is needed, are:
//
//  1. filepath.Abs — a relative root such as ".devin" must not get a different
//     record than the absolute path of the same directory.
//  2. filepath.Clean (implied by Abs) — collapses "." and ".." segments and
//     strips trailing separators, so "/repo/.devin/" and "/repo/.devin" agree.
//  3. symlink resolution of the LONGEST EXISTING ancestor, with the
//     not-yet-created suffix re-appended. A target root frequently does not
//     exist yet on a first sync, so EvalSymlinks on the whole path would fail;
//     resolving the existing prefix still collapses the cases that matter
//     (/tmp -> /private/tmp on macOS, a symlinked home, a symlinked checkout).
//
// Case is deliberately NOT folded. On a case-insensitive filesystem
// /Users/x/Repo and /Users/x/repo are the same directory and will get two
// different records — a SPLIT, whose worst outcome is that the second spelling
// finds no state and therefore treats every entry as unmanaged (preserve
// everything, prune nothing). Note that a split is SILENT: the second spelling
// has no ownership evidence under its key, so [Store.EverRecorded] answers no
// and the absence is reported as a first use. See [ErrStateUninitialized].
// Folding case would instead make two genuinely
// distinct directories on a case-sensitive filesystem share one record — an
// ALIAS, which could let one root prune the other's entries. Splitting is
// recoverable; aliasing is destructive, so the safe direction is chosen.
func CanonicalRoot(targetRoot string) (string, error) {
	if strings.TrimSpace(targetRoot) == "" {
		return "", fmt.Errorf("ownership: empty target root")
	}
	abs, err := filepath.Abs(targetRoot)
	if err != nil {
		return "", fmt.Errorf("ownership: absolute path for %q: %w", targetRoot, err)
	}
	resolved, err := resolveExistingPrefix(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// resolveExistingPrefix resolves symlinks in the longest existing ancestor of
// abs and re-appends the remaining components.
func resolveExistingPrefix(abs string) (string, error) {
	remainder := []string{}
	current := abs
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(remainder) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, remainder[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			// A permission error or a symlink loop must not silently downgrade to
			// an unresolved path: two different roots could then share a key.
			return "", fmt.Errorf("ownership: canonicalize %q: %w", abs, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding anything that exists.
			return filepath.Clean(abs), nil
		}
		remainder = append(remainder, filepath.Base(current))
		current = parent
	}
}

// RootKey derives the state key for a canonical root.
//
// The hash input is domain-separated AND length-prefixed:
//
//	sha256("agent-marketplace/ownership/root/v1" || "\x00" || len(root) || "\x00" || root)
//
// The length prefix is what makes collisions impossible by construction rather
// than by luck: without it, a scheme that concatenated fields could be fooled
// into producing the same byte string from two different (field, value) splits.
// With exactly one variable-length field, length-prefixed, the pre-image maps
// one-to-one onto the canonical path, so two distinct canonical roots have
// distinct hash inputs and only a SHA-256 collision could equate them.
//
// A hash is still a lossy name, so the record additionally stores its own
// canonical root and [Store.Load] refuses a record whose stored root differs
// from the requested one (ownership.state-root-mismatch). A key collision
// therefore fails closed instead of aliasing two roots' ownership.
func RootKey(canonicalRoot string) string {
	h := sha256.New()
	h.Write([]byte(keyDomain))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(len(canonicalRoot))))
	h.Write([]byte{0})
	h.Write([]byte(canonicalRoot))
	return hex.EncodeToString(h.Sum(nil))
}

// Digest returns the content digest used everywhere in this package. It is
// always a digest OF content, never content: no caller can recover the bytes
// from it, which is what lets a hook payload or an MCP header participate in an
// identity without entering state.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fileSlug names the safety-backup directory for one canonical output file.
// It is a digest of the output-relative path plus the sanitized base name: the
// digest keeps two same-named files in different subdirectories apart, and the
// base name keeps the directory human-recognizable. Neither component can carry
// a secret, because both are derived from a canonical output path that the
// target contract fixes (hooks.v1.json, config.json, mcp_config.json).
func fileSlug(rel string) string {
	sum := sha256.Sum256([]byte(rel))
	base := filepath.Base(rel)
	var safe strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			safe.WriteRune(r)
		default:
			safe.WriteByte('_')
		}
	}
	return hex.EncodeToString(sum[:8]) + "-" + safe.String()
}
