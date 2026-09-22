package frozengzip

import (
	"encoding/binary"
	"hash/crc32"
	"io"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/frozengzip/flate"
)

// gzipHeader is the fixed 10-byte RFC 1952 header emitted by Go's compress/gzip
// for the canonical settings: gzip magic, deflate method, no flags (no name,
// comment, or extra fields), zero mtime, XFL 4 (BestSpeed), OS 255 (unknown).
var gzipHeader = [10]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xff}

// Writer is a deterministic gzip writer. It is byte-identical to Go 1.26's
// compress/gzip writer at BestSpeed with empty name/comment/extra fields, a
// zero mtime, and OS 255, because it wraps the frozen pre-1.27 flate
// implementation. The canonical artifact format requires those exact bytes:
// its archive digest is computed over the compressed stream.
type Writer struct {
	w           io.Writer
	wroteHeader bool
	closed      bool
	buf         [8]byte
	compressor  *flate.Writer
	digest      uint32 // CRC-32 of the uncompressed data, IEEE polynomial
	size        uint32 // Uncompressed size modulo 2^32
	err         error
}

// NewBestSpeed returns a new deterministic gzip Writer writing to w. The
// header fields of the canonical encoding are fixed: there is nothing to
// configure, and callers must not depend on any way to vary them.
func NewBestSpeed(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write writes the compressed form of p to the underlying writer. Like
// compress/gzip, the 10-byte header is written lazily on the first call, so
// Close on a fresh Writer still emits a complete (empty) gzip stream.
func (z *Writer) Write(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}
	var n int
	if !z.wroteHeader {
		z.wroteHeader = true
		if _, z.err = z.w.Write(gzipHeader[:]); z.err != nil {
			return 0, z.err
		}
		z.compressor, _ = flate.NewWriter(z.w, flate.BestSpeed)
	}
	z.size += uint32(len(p))
	z.digest = crc32.Update(z.digest, crc32.IEEETable, p)
	n, z.err = z.compressor.Write(p)
	return n, z.err
}

// Close flushes any unwritten data to the underlying writer and writes the
// 8-byte gzip trailer (CRC-32 and uncompressed size). It does not close the
// underlying writer.
func (z *Writer) Close() error {
	if z.err != nil {
		return z.err
	}
	if z.closed {
		return nil
	}
	z.closed = true
	if !z.wroteHeader {
		if _, z.err = z.Write(nil); z.err != nil {
			return z.err
		}
	}
	z.err = z.compressor.Close()
	if z.err != nil {
		return z.err
	}
	binary.LittleEndian.PutUint32(z.buf[:4], z.digest)
	binary.LittleEndian.PutUint32(z.buf[4:8], z.size)
	_, z.err = z.w.Write(z.buf[:8])
	return z.err
}
