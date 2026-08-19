package analyze

import (
	"testing"
)

func TestScanAssets(t *testing.T) {
	files := FileSet{
		"clean.txt":           []byte("hello world"),
		"script":              []byte("#!/bin/bash\necho hi"),
		"elf":                 []byte{0x7f, 'E', 'L', 'F'},
		"../traversal":        []byte("bad"),
		"binary_with_control": []byte("\x00\x01\x02"),
	}

	findings := ScanAssets(files, "test")

	codes := make(map[string]int)
	for _, f := range findings {
		codes[f.Code]++
	}

	if codes[FindingExecutableAsset] != 2 {
		t.Fatalf("expected 2 executable findings, got %d", codes[FindingExecutableAsset])
	}
	if codes[FindingSuspiciousPath] != 1 {
		t.Fatalf("expected 1 suspicious path finding, got %d", codes[FindingSuspiciousPath])
	}
}

func TestExecutableMagic(t *testing.T) {
	cases := []struct {
		data []byte
		want bool
	}{
		{[]byte("#!/bin/sh\n"), true},
		{[]byte{0x7f, 'E', 'L', 'F'}, true},
		{[]byte{'M', 'Z', 0x00, 0x00}, false}, // not a valid PE
		{[]byte("hello"), false},
		{[]byte{0xcf, 0xfa, 0xed, 0xfe}, true},
	}

	for _, tc := range cases {
		_, ok := executableMagic(tc.data)
		if ok != tc.want {
			t.Fatalf("executableMagic(%q) = %v, want %v", tc.data, ok, tc.want)
		}
	}
}
