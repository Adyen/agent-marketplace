// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file carries the two definitions the frozen writer path borrows from
// Go 1.26's compress/flate inflate.go, copied verbatim. The rest of inflate.go
// implements decompression, which the canonical encoder does not use; the
// frozen package decodes through the standard library instead.

package flate

const (
	// The next three numbers come from the RFC section 3.2.7, with the
	// additional proviso in section 3.2.5 which implies that distance codes
	// 30 and 31 should never occur in compressed data.
	maxNumLit  = 286
	maxNumDist = 30
	numCodes   = 19 // number of codes in Huffman meta-code
)

// An InternalError reports an error in the flate code itself.
type InternalError string

func (e InternalError) Error() string { return "flate: internal error: " + string(e) }
