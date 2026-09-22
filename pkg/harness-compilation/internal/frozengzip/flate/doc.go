// Package flate is a frozen, verbatim copy of the writer path of Go 1.26's
// compress/flate (files deflate.go, deflatefast.go, huffman_bit_writer.go,
// huffman_code.go, and token.go, taken from the go1.26.6 release).
//
// Go 1.27 replaced the compress/flate compression algorithms, so the exact
// deflate stream produced for a given input and level is no longer stable
// across Go toolchains. compress/gzip's own documentation states that the
// exact bytes a gzip.Writer emits are not covered by the Go 1 compatibility
// promise. Canonical Marketplace artifact digests are computed over the
// gzip-compressed archive, so every artifact indexed by a binary built with
// one toolchain failed archive-digest verification on binaries built with
// another (observed in production: marketplace installs with CLI 4.0.0,
// built with Go 1.27, against artifacts indexed by server v4.0.0-rc.3, built
// with Go 1.26).
//
// The canonical artifact format therefore pins its compression to this
// frozen implementation. The copies must not be modified: any necessary
// change requires re-deriving the canonical digest definition instead.
// The files carry the original Go copyright notice and are licensed under
// the Go BSD license (see LICENSE in this directory).
package flate
