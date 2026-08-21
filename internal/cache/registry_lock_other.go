//go:build !unix

package cache

import "os"

// The non-unix half of the registry cache lock.
//
// PLATFORMS in the Makefile is darwin-arm64, darwin-amd64 and linux-amd64, and
// no release is built for anything else. This file exists so that
// `GOOS=windows go build ./...` keeps working for cross-platform editor
// analysis, not so that Windows is supported. It provides no serialization:
// acquisition reports errLockUnsupported, the caller says so in a warning and
// falls back to convergent publication.

func lockDir(*os.File) error { return errLockUnsupported }

func unlockDir(*os.File) error { return errLockUnsupported }
