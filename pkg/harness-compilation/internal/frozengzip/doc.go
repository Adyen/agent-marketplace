// Package frozengzip provides the exact gzip encoding pinned by the canonical
// Marketplace artifact format (see the artifact package in the parent tree).
//
// Canonical artifact digests are computed over the gzip-compressed archive, so
// the compressed bytes are part of the format contract, not a transport
// detail. Go 1.27 replaced the compress/flate algorithms and Go explicitly
// does not promise byte-stable gzip output, which made artifacts indexed by
// one toolchain fail verification on another. This package therefore wraps
// the frozen pre-1.27 flate implementation (subpackage flate) with the fixed
// RFC 1952 framing that Go's compress/gzip emits for the canonical settings
// (BestSpeed, no name/comment/extra, zero mtime, OS 255).
//
// The output must remain byte-identical to Go 1.26 compress/gzip for those
// settings; the artifact package's regression tests pin this with real
// production digests.
package frozengzip
