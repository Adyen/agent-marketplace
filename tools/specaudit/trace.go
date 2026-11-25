package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var annotationLine = regexp.MustCompile(`^\s*//\s*spec:\s*(\S+)\s*$`)

// skippedDirs never contain annotated tests owned by this repository.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, "testdata": true, "vendor": true}

type annotation struct {
	file string
	line int
	id   string
}

func isTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".test.ts")
}

func scanAnnotations(root string) ([]annotation, error) {
	var out []annotation
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTestFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		found, err := fileAnnotations(p, filepath.ToSlash(rel))
		out = append(out, found...)
		return err
	})
	return out, err
}

func fileAnnotations(path, rel string) ([]annotation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []annotation
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		if m := annotationLine.FindStringSubmatch(sc.Text()); m != nil {
			out = append(out, annotation{file: rel, line: n, id: m[1]})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return out, nil
}

func annotationSurface(file string) uint8 {
	switch {
	case strings.HasPrefix(file, "internal/tui/"), strings.HasPrefix(file, "tools/tui-qa/"):
		return 1
	case strings.HasPrefix(file, "gui/"), strings.HasPrefix(file, "cmd/agent-marketplace-gui/"):
		return 2
	default:
		return 0
	}
}

func trace(root string) ([]finding, string, error) {
	known, findings, err := knownScenarios(root)
	if err != nil {
		return nil, "", err
	}
	annotations, err := scanAnnotations(root)
	if err != nil {
		return nil, "", err
	}
	bound := map[string]bool{}
	surfaceBound := map[string]uint8{}
	for _, a := range annotations {
		if _, ok := known[a.id]; !ok {
			findings = append(findings, finding{kindDanglingAnnotation, fmt.Sprintf("%s:%d: %s", a.file, a.line, a.id)})
			continue
		}
		bound[a.id] = true
		if strings.HasPrefix(a.id, "session-") {
			surfaceBound[a.id] |= annotationSurface(a.file)
		}
	}
	for id, s := range known {
		if !bound[id] {
			findings = append(findings, finding{kindUnboundScenario, fmt.Sprintf("%s (%s)", id, s.pos())})
		}
		if strings.HasPrefix(s.capability, "session-") {
			for _, surface := range []struct {
				mask uint8
				name string
			}{{1, "TUI"}, {2, "GUI"}} {
				if surfaceBound[id]&surface.mask == 0 {
					findings = append(findings, finding{kindMissingSurface, fmt.Sprintf("%s missing %s witness (%s)", id, surface.name, s.pos())})
				}
			}
		}
	}
	return findings, fmt.Sprintf("%d scenarios, %d annotations", len(known), len(annotations)), nil
}
