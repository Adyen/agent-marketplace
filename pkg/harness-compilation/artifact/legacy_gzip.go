package artifact

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"hash/crc32"
	"io"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact/internal/legacyflate"
)

type legacyGzipWriter struct {
	gzip.Header
	destination io.Writer
	input       bytes.Buffer
}

func newLegacyGzipWriter(destination io.Writer) *legacyGzipWriter {
	return &legacyGzipWriter{destination: destination}
}

func (w *legacyGzipWriter) Write(p []byte) (int, error) { return w.input.Write(p) }
func (w *legacyGzipWriter) Close() error {
	raw := w.input.Bytes()
	encoded := append([]byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 4, w.OS}, legacyflate.Encode(raw)...)
	encoded = binary.LittleEndian.AppendUint32(encoded, crc32.ChecksumIEEE(raw))
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(raw)))
	n, err := w.destination.Write(encoded)
	if err == nil && n != len(encoded) {
		err = io.ErrShortWrite
	}
	return err
}
