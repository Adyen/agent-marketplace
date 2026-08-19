package analyze

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// ScanAssets examines FileSet entries for executable magic bytes, shebang lines,
// and suspicious file paths.
func ScanAssets(files FileSet, analyzer string) []Finding {
	var findings []Finding

	for path, data := range files {
		findings = append(findings, ScanSuspiciousPaths(path, path, analyzer)...)

		if len(data) == 0 {
			continue
		}

		if magic, ok := executableMagic(data); ok {
			msg := fmt.Sprintf("executable asset: %s", magic)
			findings = append(findings, NewFinding(FindingExecutableAsset, msg, analyzer, &SourceRange{File: path}))
		}
	}

	return findings
}

// executableMagic recognizes common executable / interpreted file signatures.
func executableMagic(data []byte) (string, bool) {
	if len(data) >= 2 && bytes.Equal(data[:2], []byte("#!")) {
		return "shebang script", true
	}
	if len(data) >= 4 {
		magic := data[:4]
		switch {
		case bytes.Equal(magic, []byte{0x7f, 'E', 'L', 'F'}):
			return "ELF binary", true
		case bytes.Equal(magic, []byte{'M', 'Z'}):
			if isPE(data) {
				return "PE executable", true
			}
		case bytes.Equal(magic, []byte{0xcf, 0xfa, 0xed, 0xfe}),
			bytes.Equal(magic, []byte{0xce, 0xfa, 0xed, 0xfe}),
			bytes.Equal(magic, []byte{0xfe, 0xed, 0xfa, 0xcf}),
			bytes.Equal(magic, []byte{0xfe, 0xed, 0xfa, 0xce}),
			bytes.Equal(magic, []byte{0xca, 0xfe, 0xba, 0xbe}),
			bytes.Equal(magic, []byte{0xca, 0xfe, 0xba, 0xbf}):
			return "Mach-O binary", true
		}
	}
	return "", false
}

// isPE reports whether data begins with a valid PE signature.
func isPE(data []byte) bool {
	if len(data) < 64 {
		return false
	}
	off := int(binary.LittleEndian.Uint32(data[60:64]))
	if off < 0 || off+4 > len(data) {
		return false
	}
	return bytes.Equal(data[off:off+4], []byte{'P', 'E', 0, 0})
}
