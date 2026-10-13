//go:build !unix

package ownership

import "os"

// The non-unix half of the advisory lock.
//
// This project does not target Windows, plan9, or js/wasm: PLATFORMS in the
// Makefile is darwin-arm64, darwin-amd64, and linux-amd64, and no release is
// built for anything else. This file exists so `GOOS=windows go build ./...`
// keeps working for anyone whose editor runs cross-platform analysis, NOT so
// that Windows is supported. It provides no serialization at all: acquisition
// reports [ErrLockUnsupported], the caller degrades to today's unserialized
// behaviour, and diag.CodeOwnershipLockUnavailable says so out loud.
//
// If Windows ever becomes a target, this is the one file to implement, with
// LockFileEx — and whether that justifies a golang.org/x/sys/windows dependency
// is a decision to make then, not now.

// lockOpenFlags matches the unix spelling minus O_NOFOLLOW, which is not
// portable. Nothing here ever holds a lock anyway.
const lockOpenFlags = os.O_CREATE | os.O_RDWR

func tryLockFile(*os.File) error { return ErrLockUnsupported }

func unlockFile(*os.File) error { return ErrLockUnsupported }

// isENOLCK is always false on platforms where flock(2) does not exist.
func isENOLCK(error) bool { return false }

func ReadAggregateFile(string) ([]byte, error) { return nil, ErrUnsafeAggregateFile }

// ReadAggregateFileAt refuses everywhere the descriptor-anchored unix reader
// does not exist: a platform without openat confinement gets no aggregate
// reads at all rather than a path-based substitute.
func ReadAggregateFileAt(string, string) ([]byte, error) { return nil, ErrUnsafeAggregateFile }
