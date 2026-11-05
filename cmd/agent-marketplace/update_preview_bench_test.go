package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This measures one admitted support-file read, including local disk I/O and
// root/descriptor validation, without acquisition, resolution, or network I/O.
func BenchmarkReadPreviewSupportFileDiskNoNetwork(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "support.txt"), []byte(strings.Repeat("review data\n", 100)), 0o644); err != nil {
		b.Fatal(err)
	}
	for _, keepBodies := range []bool{false, true} {
		b.Run(strconv.FormatBool(keepBodies), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := readPreviewSupportFile(root, "support.txt", keepBodies); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
