package artifact

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type legacyTarWriter struct {
	destination io.Writer
	remaining   int64
	padding     int
	err         error
}

func newLegacyTarWriter(w io.Writer) *legacyTarWriter { return &legacyTarWriter{destination: w} }
func (w *legacyTarWriter) write(p []byte) error {
	if w.err != nil {
		return w.err
	}
	n, err := w.destination.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return err
}
func (w *legacyTarWriter) flush() error {
	if w.remaining != 0 {
		return fmt.Errorf("legacy tar: missing %d bytes", w.remaining)
	}
	var zero [512]byte
	if err := w.write(zero[:w.padding]); err != nil {
		return err
	}
	w.padding = 0
	return nil
}
func (w *legacyTarWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, tar.ErrWriteTooLong
	}
	if err := w.write(p); err != nil {
		return 0, err
	}
	w.remaining -= int64(len(p))
	return len(p), nil
}
func legacyASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0 && r < 128 {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}
func legacyTarString(b []byte, s string) {
	copy(b, s)
	if len(s) > len(b) && b[len(b)-1] == '/' {
		b[len(strings.TrimRight(s[:len(b)-1], "/"))] = 0
	}
}
func legacyTarOctal(b []byte, n int64) {
	s := strconv.FormatInt(n, 8)
	copy(b, strings.Repeat("0", len(b)-len(s)-1)+s)
}
func (w *legacyTarWriter) header(name, prefix string, size, mode int64, flag byte, extended bool) error {
	if err := w.flush(); err != nil {
		return err
	}
	if size < 0 || size >= 1<<33 {
		return tar.ErrFieldTooLong
	}
	var h [512]byte
	legacyTarString(h[:100], name)
	legacyTarOctal(h[100:108], mode)
	legacyTarOctal(h[108:116], 0)
	legacyTarOctal(h[116:124], 0)
	legacyTarOctal(h[124:136], size)
	legacyTarOctal(h[136:148], 0)
	h[156] = flag
	copy(h[257:265], "ustar\x0000")
	copy(h[345:500], prefix)
	if !extended {
		legacyTarOctal(h[329:337], 0)
		legacyTarOctal(h[337:345], 0)
	}
	checksum := int64(8 * 32)
	for i, b := range h {
		if i < 148 || i >= 156 {
			checksum += int64(b)
		}
	}
	legacyTarOctal(h[148:155], checksum)
	h[155] = ' '
	if err := w.write(h[:]); err != nil {
		return err
	}
	w.remaining = size
	w.padding = int((-size) & 511)
	return nil
}
func (w *legacyTarWriter) WriteHeader(h *tar.Header) error {
	if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
		return tar.ErrHeader
	}
	if strings.ContainsRune(h.Name, 0) || h.Name == "" || strings.HasSuffix(h.Name, "/") && h.Typeflag == tar.TypeReg {
		return tar.ErrHeader
	}
	if h.Mode != 0o644 && h.Mode != 0o755 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || !h.ModTime.IsZero() {
		return tar.ErrHeader
	}
	name, prefix := h.Name, ""
	ascii := legacyASCII(name) == name
	if ascii && len(name) > 100 {
		length := min(len(name), 156)
		if length == len(name) && name[length-1] == '/' {
			length--
		}
		i := strings.LastIndex(name[:length], "/")
		if i > 0 && len(name)-i-1 <= 100 && len(name)-i-1 > 0 {
			prefix, name = name[:i], name[i+1:]
		}
	}
	if !ascii || len(name) > 100 {
		record := " path=" + h.Name + "\n"
		n := len(record) + 1
		for len(strconv.Itoa(n))+len(record) != n {
			n = len(strconv.Itoa(n)) + len(record)
		}
		record = strconv.Itoa(n) + record
		if len(record) > 1<<20 {
			return tar.ErrFieldTooLong
		}
		dir, file := path.Split(h.Name)
		paxName := legacyASCII(path.Join(dir, "PaxHeaders.0", file))
		if len(paxName) > 100 {
			paxName = paxName[:100]
		}
		paxName = strings.TrimRight(paxName, "/")
		if err := w.header(paxName, "", int64(len(record)), 0, tar.TypeXHeader, true); err != nil {
			return err
		}
		if _, err := w.Write([]byte(record)); err != nil {
			return err
		}
		name, prefix = legacyASCII(h.Name), ""
	}
	return w.header(name, prefix, h.Size, h.Mode, h.Typeflag, false)
}
func (w *legacyTarWriter) Close() error {
	if err := w.flush(); err != nil {
		return err
	}
	var zero [1024]byte
	return w.write(zero[:])
}
