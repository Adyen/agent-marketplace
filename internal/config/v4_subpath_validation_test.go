package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// Freeze the former validator; public cases below author their own expectations.
func legacyValidateV4Subpath(value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsRune(value, '\x00') || filepath.IsAbs(value) {
		return errors.New("must be a relative slash-separated path")
	}
	p := strings.ReplaceAll(value, "\\", "/")
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." && p != "." {
		return errors.New("must not escape the source root")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("must contain only normal path segments")
		}
	}
	return nil
}

func TestV4SubpathValidation(t *testing.T) {
	for _, tc := range []struct{ value, msg string }{
		{"", ""}, {"resources", ""}, {"resources/team/skills", ""},
		{`resources\team\skills`, ""}, {".hidden/resources", ""},
		{".../resources", ""}, {"resources/team with spaces", ""},
		{" resources/team ", ""}, {"resources/\xff", ""},
		{strings.Repeat("segment/", 1024) + "skills", ""},
		{"/resources", "must be a relative slash-separated path"},
		{"/../resources", "must be a relative slash-separated path"},
		{"../\x00", "must be a relative slash-separated path"},
		{"resources/\x00", "must be a relative slash-separated path"},
		{"..", "must not escape the source root"},
		{"../resources", "must not escape the source root"},
		{`..\resources`, "must not escape the source root"},
		{"resources/../../skills", "must not escape the source root"},
		{"a//../../b", "must not escape the source root"},
		{"./", "must not escape the source root"},
		{".", "must contain only normal path segments"},
		{"./resources", "must contain only normal path segments"},
		{"resources/./skills", "must contain only normal path segments"},
		{"resources/../skills", "must contain only normal path segments"},
		{`resources\..\skills`, "must contain only normal path segments"},
		{`resources\\skills`, "must contain only normal path segments"},
		{`\resources`, "must contain only normal path segments"},
		{"resources//skills", "must contain only normal path segments"},
		{"resources/skills/", "must contain only normal path segments"},
	} {
		t.Run(fmt.Sprintf("%q", tc.value), func(t *testing.T) {
			err := validateV4Subpath(tc.value)
			if tc.msg == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.msg {
				t.Fatalf("private refusal = %v, want %q", err, tc.msg)
			}
		})
	}
}

func FuzzV4SubpathValidation(f *testing.F) {
	for _, value := range []string{
		"", "resources", "resources/team/skills", `resources\team\skills`,
		".", "./", "../skills", "a//../../b", "resources/../skills",
		`resources\\skills`, "/skills", "\\skills", "\x00", "\xff",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		got, want := validateV4Subpath(value), legacyValidateV4Subpath(value)
		if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
			t.Fatalf("subpath %q: got %v, want %v", value, got, want)
		}
	})
}

func TestV4SubpathValidationPublic(t *testing.T) {
	for _, tc := range []struct{ value, normalized, msg string }{
		{"", "", ""}, {"resources", "resources", ""},
		{"resources/team/skills", "resources/team/skills", ""},
		{`resources\team\skills`, "resources/team/skills", ""},
		{` resources\team\skills `, "resources/team/skills", ""},
		{".hidden/...", ".hidden/...", ""},
		{"/skills", "", "must be a relative slash-separated path"},
		{"/../skills", "", "must be a relative slash-separated path"},
		{"../\x00", "", "must be a relative slash-separated path"},
		{"../skills", "", "must not escape the source root"},
		{"a//../../b", "", "must not escape the source root"},
		{"./", "", "must not escape the source root"},
		{".", "", "must contain only normal path segments"},
		{"resources/./skills", "", "must contain only normal path segments"},
		{"resources/../skills", "", "must contain only normal path segments"},
		{`resources\..\skills`, "", "must contain only normal path segments"},
		{`resources\\skills`, "", "must contain only normal path segments"},
		{`\skills`, "", "must contain only normal path segments"},
		{"resources//skills", "", "must contain only normal path segments"},
		{"resources/skills/", "", "must contain only normal path segments"},
	} {
		t.Run(fmt.Sprintf("%q", tc.value), func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "config.yaml")
			data := []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: https://example.test/tools.git, ref: main, path: %q}\nresources: {tools: [skill/review]}\n", tc.value))
			if err := os.WriteFile(file, data, 0o640); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, consumer := range []struct {
				name string
				run  func() (*V4Config, error)
			}{
				{"parse", func() (*V4Config, error) { return ParseV4Config(data, file, true) }},
				{"load", func() (*V4Config, error) { return LoadV4ConfigFromPath(file) }},
			} {
				t.Run(consumer.name, func(t *testing.T) {
					cfg, err := consumer.run()
					if tc.msg == "" {
						if err != nil || cfg == nil || len(cfg.Sources) != 1 || len(cfg.Requests) != 1 ||
							cfg.Sources["tools"].Kind != V4SourceGit || cfg.Sources["tools"].Path != tc.normalized ||
							cfg.Requests[0].SourceName != "tools" || cfg.Requests[0].Locator != "skill/review" {
							t.Fatalf("parsed subpath changed source binding or selection: %#v, %v", cfg, err)
						}
					} else {
						var refusal *V4ValidationError
						if cfg != nil || !errors.As(err, &refusal) || refusal.Code != "v4.git-path" ||
							refusal.Path != "sources.tools.path" || refusal.Msg != tc.msg ||
							refusal.File != file || refusal.Line != 3 {
							t.Fatalf("public refusal = %#v (%v), want v4.git-path at %s:3:sources.tools.path", refusal, err, file)
						}
					}
				})
			}
			after, readErr := os.ReadFile(file)
			afterInfo, statErr := os.Stat(file)
			entries, dirErr := os.ReadDir(dir)
			if readErr != nil || statErr != nil || dirErr != nil || !bytes.Equal(after, data) ||
				afterInfo.Mode() != info.Mode() || !afterInfo.ModTime().Equal(info.ModTime()) ||
				len(entries) != 1 || entries[0].Name() != "config.yaml" {
				t.Fatalf("parse/load changed input bytes, metadata, or directory contents: %v, %v, %v", readErr, statErr, dirErr)
			}
		})
	}
}
