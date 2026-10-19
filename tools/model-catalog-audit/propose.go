package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var reviewOnlyCodes = []string{"catalog.model-not-observed", "catalog.reasoning-not-observed", "catalog.family-pin-review"}

type baselineChange struct {
	name, oldSHA, newSHA, url string
}

type stub struct {
	harness, key, model, url string
	reasoning                []string
}

type proposal struct {
	baselines  []baselineChange
	skipped    []string
	stubs      []stub
	manual     []string
	collisions []string
}

type keyIndex struct {
	byID      map[string]string
	providers map[string]map[string]bool
}

func proposeChanges(root string, r report, out io.Writer) error {
	checked, err := time.Parse(time.RFC3339, r.CheckedAt)
	if err != nil {
		return fmt.Errorf("invalid report checked_at: %w", err)
	}
	var p proposal
	updates := p.planBaselines(r, checked.Format(time.DateOnly))
	cs, err := loadCatalogs(os.DirFS(root))
	if err != nil {
		return err
	}
	p.planStubs(r, cs)
	if len(p.baselines) == 0 && len(p.stubs) == 0 {
		return nil
	}
	if len(updates) > 0 {
		if err := applyBaselines(root, updates); err != nil {
			return err
		}
	}
	if err := applyStubs(root, p.stubs); err != nil {
		return err
	}
	return writeProposal(out, r, p)
}

func (p *proposal) planBaselines(r report, checked string) map[string]baseline {
	updates := map[string]baseline{}
	for _, s := range r.Sources {
		if s.Status == "unknown" || !hasFinding(s, "source.evidence-changed") {
			continue
		}
		if !validDigest(s.ObservedSHA256) {
			p.skipped = append(p.skipped, s.Name)
			continue
		}
		updates[s.Name] = baseline{Checked: checked, SHA256: s.ObservedSHA256}
		p.baselines = append(p.baselines, baselineChange{s.Name, s.Baseline.SHA256, s.ObservedSHA256, s.URL})
	}
	return updates
}

func (p *proposal) planStubs(r report, cs []catalog) {
	idx := newKeyIndex(cs)
	existing := map[string]map[string]bool{}
	for _, c := range cs {
		existing[c.Harness] = catalogKeys(c)
	}
	seen := map[string]bool{}
	for _, s := range r.Sources {
		if s.CatalogDomain != "" {
			continue
		}
		for _, f := range s.Findings {
			if f.Code != "catalog.model-missing" || seen[s.Harness+" "+f.Model] {
				continue
			}
			seen[s.Harness+" "+f.Model] = true
			key, ok := idx.resolve(f.Model)
			if !modelID.MatchString(f.Model) || !ok {
				p.manual = append(p.manual, fmt.Sprintf("%s `%s`", html.EscapeString(s.Harness), html.EscapeString(f.Model)))
				continue
			}
			if existing[s.Harness][key] {
				p.collisions = append(p.collisions, fmt.Sprintf("%s `%s`", html.EscapeString(s.Harness), html.EscapeString(key)))
				continue
			}
			st := stub{harness: s.Harness, key: key, model: f.Model, url: s.URL}
			if s.Harness == "factory" {
				st.reasoning = reasoningLevels(observedEfforts(s, f.Model))
			}
			p.stubs = append(p.stubs, st)
		}
	}
}

func hasFinding(s sourceReport, code string) bool {
	return slices.ContainsFunc(s.Findings, func(f finding) bool { return f.Code == code })
}

func observedEfforts(s sourceReport, id string) []string {
	for _, m := range s.Observed.Models {
		if m.ID == id {
			return m.Efforts
		}
	}
	return nil
}

func reasoningLevels(efforts []string) []string {
	var levels []string
	for _, level := range []string{"low", "medium", "high"} {
		if slices.Contains(efforts, level) {
			levels = append(levels, level)
		}
	}
	return levels
}

func catalogKeys(c catalog) map[string]bool {
	keys := map[string]bool{}
	for key := range c.Models {
		keys[key] = true
	}
	for key := range c.Families {
		keys[key] = true
	}
	return keys
}

func firstSegment(id string) string {
	first, _, _ := strings.Cut(id, "-")
	return first
}

func newKeyIndex(cs []catalog) keyIndex {
	idx := keyIndex{byID: map[string]string{}, providers: map[string]map[string]bool{}}
	for _, c := range cs {
		for key := range catalogKeys(c) {
			if _, id, ok := strings.Cut(key, "/"); ok {
				if prev, seen := idx.byID[id]; !seen || key < prev {
					idx.byID[id] = key
				}
			}
		}
		for key := range c.Models {
			provider, id, ok := strings.Cut(key, "/")
			if !ok {
				continue
			}
			first := firstSegment(id)
			if idx.providers[first] == nil {
				idx.providers[first] = map[string]bool{}
			}
			idx.providers[first][provider] = true
		}
	}
	return idx
}

func (idx keyIndex) resolve(id string) (string, bool) {
	if key, ok := idx.byID[id]; ok {
		return key, true
	}
	providers := idx.providers[firstSegment(id)]
	if len(providers) != 1 {
		return "", false
	}
	for provider := range providers {
		return provider + "/" + id, true
	}
	return "", false
}

func loadCatalogs(files fs.FS) ([]catalog, error) {
	names, err := fs.Glob(files, catalogDir+"/*.yaml")
	if err != nil {
		return nil, err
	}
	var cs []catalog
	for _, name := range names {
		body, err := readBounded(files, name)
		if err != nil {
			return nil, err
		}
		var c catalog
		if err := yaml.Unmarshal(body, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		cs = append(cs, c)
	}
	return cs, nil
}

func applyBaselines(root string, updates map[string]baseline) error {
	body, err := readBounded(os.DirFS(root), baselinesPath)
	if err != nil {
		return err
	}
	text := string(body)
	for _, name := range slices.Sorted(maps.Keys(updates)) {
		pattern := regexp.MustCompile(`(?m)^([ \t]*"` + regexp.QuoteMeta(name) + `": \{"checked": ")[^"]*(", "sha256": ")[^"]*(")`)
		if len(pattern.FindAllStringIndex(text, -1)) != 1 {
			return fmt.Errorf("baseline %s must match exactly one line", name)
		}
		b := updates[name]
		text = pattern.ReplaceAllString(text, "${1}"+b.Checked+"${2}"+b.SHA256+"${3}")
	}
	var baselines map[string]baseline
	if err := json.Unmarshal([]byte(text), &baselines); err != nil {
		return fmt.Errorf("decode updated baselines: %w", err)
	}
	if err := validateBaselines(baselines, sources); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, baselinesPath), []byte(text), 0o644)
}

func applyStubs(root string, stubs []stub) error {
	byHarness := map[string][]stub{}
	for _, s := range stubs {
		byHarness[s.harness] = append(byHarness[s.harness], s)
	}
	files := os.DirFS(root)
	for _, harness := range slices.Sorted(maps.Keys(byHarness)) {
		name := catalogPath(harness)
		body, err := readBounded(files, name)
		if err != nil {
			return err
		}
		group := byHarness[harness]
		updated, err := insertStubs(body, group)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		var c catalog
		if err := yaml.Unmarshal(updated, &c); err != nil {
			return fmt.Errorf("%s: stubs do not parse: %w", name, err)
		}
		for _, s := range group {
			if _, ok := c.Models[s.key]; !ok {
				return fmt.Errorf("%s: stub %s missing after insertion", name, s.key)
			}
		}
		if err := os.WriteFile(filepath.Join(root, name), updated, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func insertStubs(body []byte, stubs []stub) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	start := slices.Index(lines, "models:")
	if start < 0 {
		return nil, fmt.Errorf("no top-level models: block")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if l := lines[i]; l != "" && l[0] != ' ' && l[0] != '#' {
			end = i
			break
		}
	}
	for end > start+1 && (strings.TrimSpace(lines[end-1]) == "" || strings.HasPrefix(lines[end-1], "#")) {
		end--
	}
	slices.SortFunc(stubs, func(a, b stub) int { return strings.Compare(a.key, b.key) })
	var added []string
	for _, s := range stubs {
		added = append(added, stubLines(s)...)
	}
	updated := slices.Concat(lines[:end], added, lines[end:])
	return []byte(strings.Join(updated, "\n")), nil
}

func stubLines(s stub) []string {
	lines := []string{
		"  # TODO(model-catalog-drift): stub from " + s.url + "; verify native resource support and reasoning before merging.",
		"  " + s.key + ":",
		"    model: " + s.model,
	}
	if len(s.reasoning) > 0 {
		var levels []string
		for _, level := range s.reasoning {
			levels = append(levels, fmt.Sprintf("%s: {effort: %s}", level, level))
		}
		lines = append(lines, "    reasoning: {"+strings.Join(levels, ", ")+"}")
	}
	return lines
}

func writeProposal(out io.Writer, r report, p proposal) error {
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "## Model catalog drift proposal\n\nChecked: %s\n\n", esc(r.CheckedAt))
	section := func(title string, items []string) {
		fmt.Fprintf(&b, "### %s\n\n", title)
		if len(items) == 0 {
			b.WriteString("None.\n\n")
			return
		}
		for _, item := range items {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}
	var baselineItems, stubItems, reviewItems, unknownItems, skippedItems []string
	for _, c := range p.baselines {
		baselineItems = append(baselineItems, fmt.Sprintf("**%s**: `%s` → `%s` ([source](%s))", esc(c.name), short(c.oldSHA), short(c.newSHA), esc(c.url)))
	}
	for _, name := range p.skipped {
		skippedItems = append(skippedItems, fmt.Sprintf("**%s**: observed digest is not a 64-character lowercase hex value; baseline unchanged", esc(name)))
	}
	for _, s := range p.stubs {
		stubItems = append(stubItems, fmt.Sprintf("%s `%s` (model `%s`) from %s", esc(s.harness), esc(s.key), esc(s.model), esc(s.url)))
	}
	for _, s := range r.Sources {
		if s.Status == "unknown" {
			unknownItems = append(unknownItems, fmt.Sprintf("**%s**: no freshness conclusion (%s)", esc(s.Name), esc(s.URL)))
		}
		for _, f := range s.Findings {
			if slices.Contains(reviewOnlyCodes, f.Code) {
				reviewItems = append(reviewItems, fmt.Sprintf("**%s** %s `%s`: %s", esc(s.Name), esc(f.Code), esc(f.Model), esc(f.Message)))
			}
		}
	}
	section("Evidence baselines refreshed", baselineItems)
	section("Catalog stubs added", stubItems)
	section("Needs a manual catalog entry", p.manual)
	section("Collisions with existing keys (not changed)", p.collisions)
	section("Review only (not changed automatically)", reviewItems)
	section("Sources without a freshness conclusion", unknownItems)
	section("Baselines skipped", skippedItems)
	b.WriteString("### Reviewer checklist\n\n")
	b.WriteString("- [ ] Verify each stub against first-party documentation.\n")
	b.WriteString("- [ ] Add reasoning and aliases where the native resource format requires them.\n")
	b.WriteString("- [ ] Add conversion fixtures per `pkg/harness-compilation/README.md` → \"Update a shipped model catalog\".\n")
	b.WriteString("- [ ] Run `make model-catalog-check`.\n\n")
	b.WriteString("This PR was opened with GITHUB_TOKEN, so CI does not run automatically; push a commit (or close and reopen) to trigger checks.\n")
	_, err := io.WriteString(out, b.String())
	return err
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}
