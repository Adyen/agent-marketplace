//go:build unix

package registryfs

import "syscall"

// openNonBlock keeps opening a registry-controlled path from blocking. A plain
// open(2) of a FIFO for reading waits for a writer forever, which turns a
// registry file into a sync hang before any type check can refuse it. With
// O_NONBLOCK the open returns immediately and fstat reveals the FIFO. The flag
// has no effect on the regular files this package actually reads.
const openNonBlock = syscall.O_NONBLOCK
