package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	specsDir   = "openspec/specs"
	changesDir = "openspec/changes"
	archiveDir = "archive"
	specFile   = "spec.md"
)

type scenario struct {
	capability string
	title      string
	file       string
	line       int
}

func (s scenario) slug() string { return slugify(s.title) }
func (s scenario) id() string   { return s.capability + "/" + s.slug() }
func (s scenario) pos() string  { return fmt.Sprintf("%s:%d", s.file, s.line) }

type requirement struct {
	name      string
	scenarios []scenario
}

// section groups the requirements under one "## ..." heading. op is empty for
// a main spec's "## Requirements" and ADDED/MODIFIED/REMOVED/RENAMED in deltas.
type section struct {
	op           string
	requirements []requirement
}

var (
	deltaHeading       = regexp.MustCompile(`^## (ADDED|MODIFIED|REMOVED|RENAMED) Requirements\s*$`)
	requirementHeading = regexp.MustCompile(`^### Requirement:\s*(.+?)\s*$`)
	scenarioHeading    = regexp.MustCompile(`^#### Scenario:\s*(.+?)\s*$`)
)

func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func normalizeName(name string) string { return strings.Join(strings.Fields(name), " ") }

// parseSpec reads the requirement and scenario headings of a main spec or a
// change delta, ignoring fenced code blocks and non-requirement sections.
func parseSpec(root, rel, capability string) ([]section, error) {
	f, err := os.Open(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var (
		sections []section
		cur      *section
		fenced   bool
		lineNo   int
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		switch {
		case strings.HasPrefix(line, "## "):
			cur = nil
			if m := deltaHeading.FindStringSubmatch(line); m != nil {
				sections = append(sections, section{op: m[1]})
			} else if strings.TrimSpace(line) == "## Requirements" {
				sections = append(sections, section{})
			} else {
				continue
			}
			cur = &sections[len(sections)-1]
		case cur == nil:
		case requirementHeading.MatchString(line):
			name := requirementHeading.FindStringSubmatch(line)[1]
			cur.requirements = append(cur.requirements, requirement{name: normalizeName(name)})
		case scenarioHeading.MatchString(line) && len(cur.requirements) > 0:
			r := &cur.requirements[len(cur.requirements)-1]
			r.scenarios = append(r.scenarios, scenario{
				capability: capability,
				title:      scenarioHeading.FindStringSubmatch(line)[1],
				file:       rel,
				line:       lineNo,
			})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return sections, nil
}

// specFiles returns capability path -> repository-relative spec file for every
// spec.md below dir.
func specFiles(root, dir string) (map[string]string, error) {
	out := map[string]string{}
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != specFile {
			return err
		}
		capDir, err := filepath.Rel(base, filepath.Dir(p))
		if err != nil {
			return err
		}
		out[filepath.ToSlash(capDir)] = filepath.ToSlash(filepath.Join(dir, capDir, specFile))
		return nil
	})
	return out, err
}

// activeChanges lists change directories below openspec/changes, excluding the
// archive, in name order.
func activeChanges(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, changesDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != archiveDir {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// capabilitySpec keeps a capability's requirements in document order.
type capabilitySpec []requirement

func (c capabilitySpec) index(name string) int {
	for i, r := range c {
		if r.name == name {
			return i
		}
	}
	return -1
}

// knownScenarios returns the effective scenario set: main specs, with every
// active change's ADDED/MODIFIED requirements applied and REMOVED ones dropped.
// Duplicate slugs within a capability are reported as findings.
func knownScenarios(root string) (map[string]scenario, []finding, error) {
	caps := map[string]capabilitySpec{}
	mains, err := specFiles(root, specsDir)
	if err != nil {
		return nil, nil, err
	}
	for capability, rel := range mains {
		sections, err := parseSpec(root, rel, capability)
		if err != nil {
			return nil, nil, err
		}
		for _, s := range sections {
			caps[capability] = append(caps[capability], s.requirements...)
		}
	}

	changes, err := activeChanges(root)
	if err != nil {
		return nil, nil, err
	}
	for _, change := range changes {
		deltas, err := specFiles(root, filepath.ToSlash(filepath.Join(changesDir, change, "specs")))
		if err != nil {
			return nil, nil, err
		}
		for _, capability := range sortedKeys(deltas) {
			sections, err := parseSpec(root, deltas[capability], capability)
			if err != nil {
				return nil, nil, err
			}
			for _, s := range sections {
				caps[capability] = applyDelta(caps[capability], s)
			}
		}
	}

	known := map[string]scenario{}
	var findings []finding
	for _, capability := range sortedKeys(caps) {
		for _, r := range caps[capability] {
			for _, s := range r.scenarios {
				if prev, dup := known[s.id()]; dup {
					findings = append(findings, finding{kindDuplicateSlug, fmt.Sprintf(
						"%s: %q (%s) and %q (%s)", capability, prev.title, prev.pos(), s.title, s.pos())})
					continue
				}
				known[s.id()] = s
			}
		}
	}
	return known, findings, nil
}

func applyDelta(c capabilitySpec, s section) capabilitySpec {
	for _, r := range s.requirements {
		i := c.index(r.name)
		switch s.op {
		case "ADDED":
			c = append(c, r)
		case "MODIFIED":
			if i < 0 {
				c = append(c, r)
			} else {
				c[i] = r
			}
		case "REMOVED":
			if i >= 0 {
				c = append(c[:i:i], c[i+1:]...)
			}
		}
	}
	return c
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
