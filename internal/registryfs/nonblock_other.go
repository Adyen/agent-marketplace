//go:build !unix

package registryfs

// Windows has no O_NONBLOCK for file opens, and its FIFO equivalent (a named
// pipe) lives in a device namespace an os.Root-relative path cannot reach, so
// the type check on the opened descriptor is sufficient there.
const openNonBlock = 0
