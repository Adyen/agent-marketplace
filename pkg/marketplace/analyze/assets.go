package analyze

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"unicode"
)

// executableExtensions is the set of file extensions that strongly indicate an
// executable or interpreted script, regardless of contents.
var executableExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".sh": true, ".bash": true, ".csh": true, ".zsh": true, ".fish": true,
	".ksh": true, ".tcsh": true,
	".py": true, ".rb": true, ".pl": true, ".ps1": true, ".bat": true,
	".cmd": true, ".com": true, ".awk": true, ".sed": true, ".php": true,
	".lua": true, ".js": true, ".mjs": true, ".vbs": true, ".wsf": true,
	".jar": true, ".class": true, ".run": true, ".app": true, ".wasm": true,
	".msi": true, ".scr": true, ".pif": true, ".o": true, ".a": true,
	".lib": true,
}

var scriptExtensions = map[string]bool{
	".sh": true, ".bash": true, ".csh": true, ".zsh": true, ".fish": true,
	".ksh": true, ".tcsh": true, ".py": true, ".rb": true, ".pl": true,
	".ps1": true, ".bat": true, ".cmd": true, ".awk": true, ".sed": true,
	".php": true, ".lua": true, ".js": true, ".mjs": true, ".vbs": true,
	".wsf": true,
}

// ScanAssets examines FileSet entries for executable magic bytes, shebang lines,
// suspicious file paths, and executable file extensions.
func ScanAssets(files FileSet, analyzer string) []Finding {
	var findings []Finding

	for filePath, data := range files {
		findings = append(findings, ScanSuspiciousFilePaths(filePath, filePath, analyzer)...)

		// Normalize path separators so extension detection is portable.
		normPath := strings.ReplaceAll(filePath, "\\", "/")
		scriptDetected := false
		if ext, ok := extensionExecutable(normPath); ok {
			code := FindingExecutableAsset
			if scriptExtensions[ext] {
				code = FindingExecutableScript
				scriptDetected = true
			}
			msg := fmt.Sprintf("executable asset: extension %s", ext)
			slog.Debug("executable asset detected", "path", filePath, "reason", msg)
			findings = append(findings, NewFinding(code, msg, analyzer, &SourceRange{File: filePath}))
		}

		if len(data) == 0 {
			continue
		}

		if magic, ok := executableMagic(data); ok {
			code := FindingExecutableAsset
			if magic == "shebang script" {
				if scriptDetected {
					continue
				}
				code = FindingExecutableScript
			}
			msg := fmt.Sprintf("executable asset: %s", magic)
			slog.Debug("executable asset detected", "path", filePath, "reason", msg)
			findings = append(findings, NewFinding(code, msg, analyzer, &SourceRange{File: filePath}))
		}
	}

	return findings
}

// executableMagic recognizes common executable / interpreted file signatures.
func executableMagic(data []byte) (string, bool) {
	if hasShebang(data) {
		return "shebang script", true
	}
	if len(data) >= 4 {
		magic := data[:4]
		switch {
		case bytes.Equal(magic, []byte{0x7f, 'E', 'L', 'F'}):
			return "ELF binary", true
		case bytes.Equal(data[:2], []byte{'M', 'Z'}):
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

// hasShebang reports whether data begins with a shebang, ignoring an optional
// leading UTF-8 BOM and any leading whitespace on the first line.
func hasShebang(data []byte) bool {
	line := data
	if idx := bytes.IndexAny(data, "\r\n"); idx >= 0 {
		line = data[:idx]
	}

	const bom = "\xEF\xBB\xBF"
	if bytes.HasPrefix(line, []byte(bom)) {
		line = line[len(bom):]
	}

	line = bytes.TrimLeftFunc(line, func(r rune) bool {
		return r != '\n' && r != '\r' && unicode.IsSpace(r)
	})

	return bytes.HasPrefix(line, []byte("#!"))
}

// extensionExecutable reports whether a file path has a known executable or
// script extension. It also recognizes versioned shared libraries such as
// libfoo.so.1.
func extensionExecutable(p string) (string, bool) {
	ext := strings.ToLower(path.Ext(p))
	if ext == "" {
		return "", false
	}
	if executableExtensions[ext] {
		return ext, true
	}
	if isNumericExtension(ext) {
		base := strings.TrimSuffix(p, ext)
		if strings.ToLower(path.Ext(base)) == ".so" {
			return ".so", true
		}
	}
	return "", false
}

// isNumericExtension reports whether ext is of the form ".123".
func isNumericExtension(ext string) bool {
	if len(ext) < 2 || ext[0] != '.' {
		return false
	}
	for i := 1; i < len(ext); i++ {
		c := ext[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
