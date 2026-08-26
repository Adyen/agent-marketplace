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

	if codes[FindingExecutableAsset] != 1 {
		t.Fatalf("expected 1 opaque executable finding, got %d", codes[FindingExecutableAsset])
	}
	if codes[FindingExecutableScript] != 1 {
		t.Fatalf("expected 1 script capability finding, got %d", codes[FindingExecutableScript])
	}
	if codes[FindingAssetPathEscape] != 1 {
		t.Fatalf("expected 1 asset path escape finding, got %d", codes[FindingAssetPathEscape])
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
		{[]byte("\xEF\xBB\xBF  #!/bin/sh\n"), true}, // BOM + leading spaces
		{[]byte(" \t#!/bin/bash\n"), true},          // leading whitespace
		{[]byte("\n#!/bin/sh\n"), false},            // shebang not on first line
		{[]byte("\xEF\xBB\xBF"), false},             // BOM only
	}

	for _, tc := range cases {
		_, ok := executableMagic(tc.data)
		if ok != tc.want {
			t.Fatalf("executableMagic(%q) = %v, want %v", tc.data, ok, tc.want)
		}
	}
}

func TestExecutableExtensionDetection(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"app.exe", ".exe"},
		{"script.sh", ".sh"},
		{"deploy.PY", ".py"},
		{"libfoo.so.1", ".so"},
		{"a/b/c.dylib", ".dylib"},
		{`windows\\app.bat`, ".bat"},
		{"clean.txt", ""},
		{"libfoo.so", ".so"},
	}

	for _, tc := range cases {
		got, ok := extensionExecutable(tc.path)
		if tc.want == "" {
			if ok {
				t.Errorf("extensionExecutable(%q) = (%q, true), want false", tc.path, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("extensionExecutable(%q) = (%q, %v), want (%q, true)", tc.path, got, ok, tc.want)
		}
	}
}

func TestScanAssetsExtensionDetection(t *testing.T) {
	files := FileSet{
		"app.exe":   []byte("not a binary"),
		"run.sh":    []byte("echo hi"),
		"lib.so.1":  []byte{0x00},
		"clean.txt": []byte("hello"),
	}

	findings := ScanAssets(files, "test")
	codes := make(map[string]int)
	for _, f := range findings {
		codes[f.Code]++
	}

	if codes[FindingExecutableAsset] != 2 {
		t.Fatalf("expected 2 opaque executable findings, got %d", codes[FindingExecutableAsset])
	}
	if codes[FindingExecutableScript] != 1 {
		t.Fatalf("expected 1 script capability finding, got %d", codes[FindingExecutableScript])
	}
}

func TestScanAssetsDoesNotDuplicateScriptEvidence(t *testing.T) {
	findings := ScanAssets(FileSet{"run.sh": []byte("#!/bin/sh\necho hi\n")}, "test")
	if len(findings) != 1 || findings[0].Code != FindingExecutableScript {
		t.Fatalf("findings = %v, want one script capability", findings)
	}
}

func TestScanAssetsBOMShebang(t *testing.T) {
	files := FileSet{
		"bom":     []byte("\xEF\xBB\xBF  #!/bin/sh\n"),
		"space":   []byte(" \t#!/bin/bash\n"),
		"newline": []byte("\n#!/bin/sh\n"),
	}

	findings := ScanAssets(files, "test")
	codes := make(map[string]int)
	for _, f := range findings {
		codes[f.Code]++
	}

	if codes[FindingExecutableScript] != 2 {
		t.Fatalf("expected 2 script capability findings, got %d", codes[FindingExecutableScript])
	}
}
