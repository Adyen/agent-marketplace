package lockfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRejectsUnsafeAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.yaml")
	data := []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\n")
	if err := os.WriteFile(valid, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(valid); err != nil {
		t.Fatalf("Read(valid): %v", err)
	}

	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(valid, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link); err == nil {
		t.Fatal("Read accepted a symlink")
	}

	oversized := filepath.Join(dir, "large.yaml")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(oversized); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("Read(oversized) error = %v", err)
	}
}

func TestDecodeRejectsOversizedInputBeforeYAMLParsing(t *testing.T) {
	data := bytes.Repeat([]byte("{"), int(MaxFileSize)+1)
	if _, err := Decode(data); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("Decode(oversized) error = %v, want ErrFileTooLarge", err)
	}
}

func TestDecodeReaderRejectsOversizedInputWithBoundedRead(t *testing.T) {
	data := bytes.Repeat([]byte("{"), int(MaxFileSize)+1)
	if _, err := DecodeReader(bytes.NewReader(data)); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("DecodeReader(oversized) error = %v, want ErrFileTooLarge", err)
	}
}

func TestDecodeRejectsExcessiveYAMLDepth(t *testing.T) {
	data := []byte(strings.Repeat("- ", maxYAMLDepth+1) + "value\n")
	var codedErr *Error
	if _, err := Decode(data); !errors.As(err, &codedErr) || codedErr.Code != CodeInvalidDocument {
		t.Fatalf("Decode(deep YAML) error = %v", err)
	}
}

func TestReadBytesInfoRegularAndDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "valid.yaml")
	data := []byte("apiVersion: agent-marketplace-lock/v1\nscope: repo\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, info, err := ReadBytesInfo(path)
	if err != nil || string(got) != string(data) || info == nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bytes=%q info=%v err=%v", got, info, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadBytesInfo(filepath.Join(dir, "directory")); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("directory error=%v", err)
	}
}
