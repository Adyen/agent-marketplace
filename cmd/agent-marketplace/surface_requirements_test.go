package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

const surfaceCredHint = "https://inert:s3cr3t@example.invalid/tools.git"

func writeSkillRequiringItems(t *testing.T, root, name, description string, items []any) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s\n", name, description)
	if len(items) > 0 {
		b.WriteString("metadata:\n  requires:\n")
		for _, item := range items {
			switch value := item.(type) {
			case string:
				fmt.Fprintf(&b, "    - %s\n", value)
			case map[string]string:
				fmt.Fprintf(&b, "    - kind: %s\n      name: %s\n      registry: %s\n", value["kind"], value["name"], value["registry"])
			default:
				t.Fatalf("unsupported requires item %T", item)
			}
		}
	}
	b.WriteString("---\n# Body\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func surfaceRepoConfig(t *testing.T, repoRoot, origin, include string, expand bool) string {
	t.Helper()
	return surfaceRepoConfigExcluding(t, repoRoot, origin, include, expand, "")
}

func surfaceRepoConfigExcluding(t *testing.T, repoRoot, origin, include string, expand bool, exclude string) string {
	t.Helper()
	requires := ""
	if expand {
		requires = "requires: {expand: true}\n"
	}
	group := "  reg: [" + include + "]\n"
	if exclude != "" {
		group = "  reg: {include: [" + include + "], exclude: [" + exclude + "]}\n"
	}
	path := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"+requires+"sources:\n  reg: {git: file://"+origin+", ref: main}\nresources:\n"+group)
	return path
}

func writeSurfaceRegistry(t *testing.T) (repoRoot, origin, working string) {
	t.Helper()
	repoRoot = t.TempDir()
	origin, working = createBareRepoWithWorkingClone(t, "surface-registry.git")
	for _, dir := range []string{"skills", "commands", "mcp", "collections"} {
		if err := os.MkdirAll(filepath.Join(working, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSkillRequiringItems(t, working, "root", "Root", []any{"skill:helper", "mcp:docs"})
	writeSkillRequiringItems(t, working, "helper", "Helper", []any{"skill:mid"})
	writeSkillRequiringItems(t, working, "mid", "Mid", []any{"skill:leaf"})
	writeSkillRequiringItems(t, working, "leaf", "Leaf", nil)
	writeSkillRequiringItems(t, working, "needy", "Needs provider", []any{"skill:helper", "skill:provider"})
	writeSkillRequiringItems(t, working, "provider", "Provider", []any{"skill:needy", "mcp:docs"})
	writeSkillRequiringItems(t, working, "hinter", "Hinted provider", []any{
		map[string]string{"kind": "skill", "name": "provider", "registry": surfaceCredHint},
		map[string]string{"kind": "skill", "name": "ghost", "registry": surfaceCredHint},
	})
	writeSkillRequiringItems(t, working, "reviewer", "Review dependent", []any{map[string]string{"kind": "skill", "name": "review", "registry": surfaceCredHint}, "skill:elsewhere", "skill:leaf"})
	writeSkillRequiringItems(t, working, "unrelated", "Unrelated", []any{"skill:udep"})
	writeSkillRequiringItems(t, working, "udep", "Unrelated dep", nil)
	writeSkillRequiringItems(t, working, "plain", "Plain", nil)
	writeSkillRequiringItems(t, working, "broken", "Broken", nil)
	if err := os.WriteFile(filepath.Join(working, "skills", "broken", "SKILL.md"), []byte("---\nname: broken\ndescription: Broken\nmetadata:\n  requires:\n    - 42\n---\n# Body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkillRequiringItems(t, working, "malformed", "Malformed", nil)
	if err := os.WriteFile(filepath.Join(working, "skills", "malformed", "SKILL.md"), []byte("---\nname: malformed\ndescription: Malformed\nmetadata:\n  requires: not-a-list\n---\n# Body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, "commands", "helper.md"), []byte("# helper command\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, ".mcp.json"), []byte(`{"mcpServers":{"docs":{"command":"docs","args":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
description: Bundle
skills:
  - name: root
  - name: helper
`)
	commitAndPushAll(t, working, "surface fixtures")
	surfaceRepoConfig(t, repoRoot, origin, "collection/bundle", true)
	return repoRoot, origin, working
}

// spec: resource-installation/enabled-install-reports-transitive-implied-prerequisites
func TestSurfaceRequirementsInstallEnabledTransitiveChain(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _, _ := writeSurfaceRegistry(t)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "bundle")
	if err != nil {
		t.Fatalf("install bundle: %v\n%s", err, output)
	}
	summaryIdx := strings.Index(output, "Installed collection bundle")
	if summaryIdx < 0 {
		t.Fatalf("missing existing install summary:\n%s", output)
	}
	for _, want := range []string{
		`also selected mcp "docs" (required by skill "root")`,
		`also selected skill "mid" (required by skill "helper")`,
		`also selected skill "leaf" (required by skill "mid")`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing implied summary %q:\n%s", want, output)
		}
		if strings.Index(output, want) < summaryIdx {
			t.Fatalf("implied summary %q precedes the existing install summary:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		`also selected skill "root"`, `also selected skill "helper"`,
		`also selected skill "needy"`, `also selected skill "provider"`,
		`also selected skill "udep"`, `also selected command`,
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("unexpected implied summary %q:\n%s", unwanted, output)
		}
	}
	if n := strings.Count(output, `also selected mcp "docs"`); n != 1 {
		t.Fatalf("implied docs reported %d times:\n%s", n, output)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "mcp.json")); err != nil {
		t.Fatalf("implied mcp resource not materialized: %v", err)
	}
	for _, name := range []string{"mid", "leaf"} {
		if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("implied skill %s not materialized: %v", name, err)
		}
	}
	configBytes, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "collection/bundle") {
		t.Fatalf("config lost the requested collection selection:\n%s", configBytes)
	}
	for _, selector := range []string{"skill/mid", "skill/leaf", "mcp/docs"} {
		if strings.Contains(string(configBytes), selector) {
			t.Fatalf("implied selector %q written to config:\n%s", selector, configBytes)
		}
	}
}

// spec: resource-installation/disabled-install-reports-a-transitive-preview
// spec: status-reporting/status-distinguishes-selected-relationships-from-previews
func TestSurfaceRequirementsInstallDisabledTransitivePreview(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	surfaceRepoConfig(t, repoRoot, origin, "collection/bundle", false)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "bundle")
	if err != nil {
		t.Fatalf("install bundle: %v\n%s", err, output)
	}
	summaryIdx := strings.Index(output, "Installed collection bundle")
	if summaryIdx < 0 {
		t.Fatalf("missing existing install summary:\n%s", output)
	}
	for _, want := range []string{
		`would also select mcp "docs" (required by skill "root")`,
		`would also select skill "mid" (required by skill "helper")`,
		`would also select skill "leaf" (required by skill "mid")`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing advisory preview %q:\n%s", want, output)
		}
		if strings.Index(output, want) < summaryIdx {
			t.Fatalf("advisory preview %q precedes the existing install summary:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		`would also select skill "root"`, `would also select skill "helper"`,
		`would also select skill "provider"`, `would also select skill "udep"`,
		`would also select command`,
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("unexpected preview %q:\n%s", unwanted, output)
		}
	}
	if strings.Contains(output, "also selected") {
		t.Fatalf("disabled mode emitted a non-advisory implied summary:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("preview mcp resource materialized")
	}
	for _, name := range []string{"mid", "leaf"} {
		if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", name)); !os.IsNotExist(err) {
			t.Fatalf("preview skill %s materialized", name)
		}
	}
	factoryDir := filepath.Join(repoRoot, ".factory")
	snapshot := ownedCommandSnapshot(t, factoryDir)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "collection/bundle") {
		t.Fatalf("config lost the requested collection selection:\n%s", configBytes)
	}
	for _, selector := range []string{"skill/mid", "skill/leaf", "mcp/docs", "requires:"} {
		if strings.Contains(string(configBytes), selector) {
			t.Fatalf("disabled install wrote %q to config:\n%s", selector, configBytes)
		}
	}

	statusOut, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	_, syncedRows := parseTable(t, statusSection(statusOut, "SYNCED RESOURCES"))
	for _, member := range []string{"|root|", "|helper|"} {
		found := false
		for _, row := range syncedRows {
			if strings.Contains(row, member) {
				found = true
			}
		}
		if !found {
			t.Fatalf("synced resources lost collection member %q:\n%s", member, statusOut)
		}
	}
	for _, preview := range []string{"|mid|", "|leaf|", "|docs|"} {
		for _, row := range syncedRows {
			if strings.Contains(row, preview) {
				t.Fatalf("preview-only resource %q shown as synced: %q", preview, row)
			}
		}
	}
	gotConfig, _ := os.ReadFile(configPath)
	gotLock, _ := os.ReadFile(lockPath)
	if string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("status rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, factoryDir)) {
		t.Fatal("status changed materialized bytes")
	}
}

// spec: resource-installation/install-summary-omits-direct-and-unrelated-resources
func TestSurfaceRequirementsInstallOmitsDirectAndUnrelated(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	surfaceRepoConfig(t, repoRoot, origin, "skill/mid, skill/unrelated, command/helper", true)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "bundle", "provider")
	if err != nil {
		t.Fatalf("install bundle provider: %v\n%s", err, output)
	}
	if !strings.Contains(output, `also selected skill "leaf" (required by skill "mid")`) {
		t.Fatalf("implied leaf behind satisfied direct provider mid missing:\n%s", output)
	}
	if !strings.Contains(output, `also selected skill "needy" (required by skill "provider")`) {
		t.Fatalf("implied needy missing:\n%s", output)
	}
	if !strings.Contains(output, `also selected mcp "docs" (required by skill "root", skill "provider")`) &&
		!strings.Contains(output, `also selected mcp "docs" (required by skill "provider", skill "root")`) {
		t.Fatalf("implied docs requirer provenance missing:\n%s", output)
	}
	for _, unwanted := range []string{
		`also selected skill "mid"`, `also selected skill "provider"`,
		`also selected skill "root"`, `also selected skill "helper"`,
		`also selected skill "unrelated"`, `also selected skill "udep"`,
		`also selected command`,
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("direct or unrelated resource in implied summary %q:\n%s", unwanted, output)
		}
	}
	if strings.Contains(output, "udep") {
		t.Fatalf("unrelated implied resource leaked into summary:\n%s", output)
	}
	if n := strings.Count(output, `also selected mcp "docs"`); n != 1 {
		t.Fatalf("implied docs reported %d times:\n%s", n, output)
	}
}

// spec: resource-installation/unavailable-install-prerequisite-keeps-its-warning
func TestSurfaceRequirementsInstallUnavailablePrerequisiteKeepsWarning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	surfaceRepoConfigExcluding(t, repoRoot, origin, "skill/reviewer", true, "skill/leaf")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "provider", "reviewer")
	if err != nil {
		t.Fatalf("install provider: %v\n%s", err, output)
	}
	for _, want := range []string{
		`requires skill "review"`,
		`requires skill "elsewhere"`,
		`skill "reviewer" requires skill "leaf", but it is not selected`,
		`required by skill "provider"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing unmet warning %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, `also selected skill "leaf"`) || strings.Contains(output, `would also select skill "leaf"`) {
		t.Fatalf("excluded prerequisite gained a selection summary:\n%s", output)
	}
	if strings.Contains(output, surfaceCredHint) || strings.Contains(output, "s3cr3t") {
		t.Fatalf("credential-bearing hint leaked:\n%s", output)
	}
	ordinary, ordinaryErr := executeCommand("sync")
	if ordinaryErr != nil {
		t.Fatalf("ordinary sync must succeed despite unmet warnings: %v\n%s", ordinaryErr, ordinary)
	}
	if !strings.Contains(ordinary, `requires skill "leaf"`) {
		t.Fatalf("ordinary sync lost the excluded-prerequisite warning:\n%s", ordinary)
	}
	for _, args := range [][]string{{"sync", "--strict"}, {"sync", "--strict", "--check"}} {
		strictOut, strictErr := executeCommand(args...)
		if strictErr == nil {
			t.Fatalf("%v must fail on unmet prerequisites:\n%s", args, strictOut)
		}
		if !errors.Is(strictErr, errStrictUnmetPrerequisites) {
			t.Fatalf("%v must fail with errStrictUnmetPrerequisites, got %v\n%s", args, strictErr, strictOut)
		}
		if strings.Contains(strictErr.Error(), "unknown flag") || strings.Contains(strictErr.Error(), "invalid argument") || strings.Contains(strictOut, "Usage:") {
			t.Fatalf("%v failed on argument validation instead of unmet prerequisites: %v\n%s", args, strictErr, strictOut)
		}
		if !strings.Contains(strictOut, `requires skill "leaf"`) {
			t.Fatalf("%v lost the unmet prerequisite warning:\n%s", args, strictOut)
		}
	}
}

// spec: resource-prerequisites/configured-list-retains-collection-and-requirement-provenance
// spec: resource-prerequisites/ad-hoc-list-shows-declarations-without-selecting-them
func TestSurfaceRequirementsListShowsProvenanceAndDeclarations(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	surfaceRepoConfig(t, repoRoot, origin, "collection/bundle, skill/broken", true)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("install", "bundle", "broken"); err != nil {
		t.Fatalf("install fixture: %v\n%s", err, output)
	}

	factoryDir := filepath.Join(repoRoot, ".factory")
	snapshot := ownedCommandSnapshot(t, factoryDir)
	materialized := map[string][]byte{}
	for _, name := range []string{"root", "helper"} {
		path := filepath.Join(factoryDir, "skills", name, "SKILL.md")
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read materialized %s: %v", path, readErr)
		}
		materialized[path] = raw
	}
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, output)
	}
	header, rows := parseTable(t, output)
	if got := header[len(header)-1]; got != "VIA" {
		t.Fatalf("configured list missing VIA column, header %v", header)
	}
	rowOf := func(name string) string {
		t.Helper()
		for _, row := range rows {
			if strings.Contains(row, "|"+name+"|") {
				return row
			}
		}
		t.Fatalf("no %q row:\n%s", name, output)
		return ""
	}
	if row := rowOf("root"); !strings.HasSuffix(row, "|collection:bundle@1.0.0") {
		t.Fatalf("root row lost collection VIA: %q", row)
	}
	if row := rowOf("skill|helper"); !strings.HasSuffix(row, "|collection:bundle@1.0.0, requires skill:root") {
		t.Fatalf("helper row lost combined provenance: %q", row)
	}
	if row := rowOf("docs"); !strings.HasSuffix(row, "|requires skill:root") {
		t.Fatalf("implied docs row lost requires VIA: %q", row)
	}
	if strings.Contains(rowOf("broken"), "requires") {
		t.Fatalf("direct-only broken row gained provenance:\n%s", output)
	}
	kindOut, err := executeCommand("list", "--kind", "skill")
	if err != nil {
		t.Fatalf("list --kind skill: %v\n%s", err, kindOut)
	}
	_, kindRows := parseTable(t, kindOut)
	helperFound := false
	for _, row := range kindRows {
		cells := strings.Split(row, "|")
		if len(cells) < 2 || cells[1] != "skill" {
			t.Fatalf("kind-filtered list kept a non-skill row: %q", row)
		}
		if cells[2] == "helper" {
			helperFound = true
			if !strings.HasSuffix(row, "|collection:bundle@1.0.0, requires skill:root") {
				t.Fatalf("kind-filtered helper row lost combined provenance: %q", row)
			}
		}
	}
	if !helperFound {
		t.Fatalf("kind-filtered list lost the helper row:\n%s", kindOut)
	}
	gotConfig, _ := os.ReadFile(configPath)
	gotLock, _ := os.ReadFile(lockPath)
	if string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("kind-filtered list rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, factoryDir)) {
		t.Fatal("configured list changed materialized bytes")
	}
	for path, want := range materialized {
		requireIdenticalBytes(t, path, want, path)
	}

	output, err = executeCommand("list", "--from", origin, "--ref", "main")
	if err != nil {
		t.Fatalf("list --from: %v\n%s", err, output)
	}
	tableStart := strings.Index(output, "REGISTRY")
	if tableStart < 0 {
		t.Fatalf("list --from produced no table:\n%s", output)
	}
	header, rows = parseTable(t, output[tableStart:])
	if got := header[len(header)-1]; got != "REQUIRES" {
		t.Fatalf("ad-hoc list missing REQUIRES column, header %v", header)
	}
	if row := rowOf("root"); !strings.HasSuffix(row, "|skill:helper, mcp:docs") {
		t.Fatalf("root row lost ordered declarations: %q", row)
	}
	if row := rowOf("skill|helper"); !strings.HasSuffix(row, "|skill:mid") {
		t.Fatalf("helper row lost its declaration: %q", row)
	}
	headerLine := strings.SplitN(output[tableStart:], "\n", 2)[0]
	requiresOffset := strings.Index(headerLine, "REQUIRES")
	if requiresOffset < 0 {
		t.Fatalf("ad-hoc header line lost REQUIRES: %q", headerLine)
	}
	for _, name := range []string{"leaf", "plain", "broken", "malformed", "udep"} {
		rowOf(name)
		raw := ""
		for _, line := range strings.Split(output[tableStart:], "\n")[1:] {
			if strings.Contains(line, " "+name+" ") {
				raw = line
			}
		}
		if raw == "" {
			t.Fatalf("ad-hoc table lost the %q row:\n%s", name, output)
		}
		if trimmed := len(strings.TrimRight(raw, " ")); trimmed > requiresOffset {
			t.Fatalf("%q row gained a declaration cell %q (REQUIRES column at %d): %q", name, raw[requiresOffset:], requiresOffset, raw)
		}
	}
	for _, diagnostic := range []string{"requires item 0", "requires must be a list"} {
		if !strings.Contains(output, diagnostic) {
			t.Fatalf("missing non-quoting diagnostic %q:\n%s", diagnostic, output)
		}
	}
	gotConfig, _ = os.ReadFile(configPath)
	gotLock, _ = os.ReadFile(lockPath)
	if string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("list --from rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, factoryDir)) {
		t.Fatal("list --from changed materialized bytes")
	}
	for path, want := range materialized {
		requireIdenticalBytes(t, path, want, path)
	}
}

// spec: status-reporting/status-distinguishes-selected-relationships-from-previews
// spec: resource-prerequisites/rows-are-sorted
// spec: resource-prerequisites/status-reports-typed-reasons-safely
func TestSurfaceRequirementsStatusShowsViaAndReasons(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("install", "bundle", "provider"); err != nil {
		t.Fatalf("install fixture: %v\n%s", err, output)
	}
	surfaceRepoConfigExcluding(t, repoRoot, origin, "collection/bundle, skill/needy, skill/unrelated, skill/reviewer, skill/hinter", true, "skill/leaf")

	factoryDir := filepath.Join(repoRoot, ".factory")
	snapshot := ownedCommandSnapshot(t, factoryDir)
	helperPath := filepath.Join(factoryDir, "skills", "helper", "SKILL.md")
	helperBytes, readErr := os.ReadFile(helperPath)
	if readErr != nil {
		t.Fatalf("read materialized helper: %v", readErr)
	}
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, output)
	}
	header, rows := parseTable(t, statusSection(output, "SYNCED RESOURCES"))
	viaIdx := -1
	for i, col := range header {
		if col == "VIA" {
			viaIdx = i
		}
	}
	if viaIdx < 0 {
		t.Fatalf("status table missing VIA column, header %v", header)
	}
	viaCell := func(row string) string {
		cells := strings.Split(row, "|")
		if viaIdx >= len(cells) {
			return ""
		}
		return cells[viaIdx]
	}
	var helperRow, midRow, leafRow, docsRow string
	for _, row := range rows {
		switch {
		case strings.HasPrefix(row, "skill|helper|"):
			helperRow = row
		case strings.Contains(row, "|mid|"):
			midRow = row
		case strings.Contains(row, "|leaf|"):
			leafRow = row
		case strings.Contains(row, "|docs|"):
			docsRow = row
		}
	}
	if got := viaCell(helperRow); !strings.Contains(got, "collection:bundle@1.0.0") ||
		!strings.Contains(got, "requires skill:root") ||
		!strings.Contains(got, "requires skill:needy") {
		t.Fatalf("helper row lost combined provenance: %q\n%s", helperRow, output)
	}
	if got := viaCell(midRow); got != "requires skill:helper" {
		t.Fatalf("implied mid row lost requires VIA: %q", midRow)
	}
	if leafRow != "" {
		t.Fatalf("excluded leaf shown as a synced selection: %q", leafRow)
	}
	if got := viaCell(docsRow); !strings.Contains(got, "requires skill:root") || !strings.Contains(got, "requires skill:provider") {
		t.Fatalf("implied docs row lost either requirer: %q", docsRow)
	}
	unmet := statusSection(output, "UNMET REQUIREMENTS")
	unmetHeader, unmetRows := parseTable(t, unmet)
	if len(unmetHeader) < 2 || unmetHeader[len(unmetHeader)-2] != "REASON" {
		t.Fatalf("unmet table missing REASON before FIX, header %v", unmetHeader)
	}
	var ghostIdx, elseIdx, leafIdx, reviewIdx = -1, -1, -1, -1
	for i, row := range unmetRows {
		switch {
		case strings.Contains(row, "skill:hinter|skill:ghost|source-not-configured"):
			ghostIdx = i
		case strings.Contains(row, "skill:reviewer|skill:elsewhere|not-found"):
			elseIdx = i
		case strings.Contains(row, "skill:reviewer|skill:leaf|excluded"):
			leafIdx = i
		case strings.Contains(row, "skill:reviewer|skill:review|source-not-configured"):
			reviewIdx = i
		}
		if !strings.Contains(row, "|agent-marketplace install ") {
			t.Fatalf("unmet row lost its FIX remediation: %q", row)
		}
		if strings.Contains(row, surfaceCredHint) || strings.Contains(row, "s3cr3t") {
			t.Fatalf("credential-bearing hint leaked:\n%s", row)
		}
	}
	if ghostIdx < 0 || elseIdx < 0 || leafIdx < 0 || reviewIdx < 0 {
		t.Fatalf("missing typed unmet rows (ghost=%d elsewhere=%d leaf=%d review=%d):\n%s", ghostIdx, elseIdx, leafIdx, reviewIdx, unmet)
	}
	if ghostIdx >= elseIdx || elseIdx >= leafIdx || leafIdx >= reviewIdx {
		t.Fatalf("unmet rows not sorted by RESOURCE then REQUIRES:\n%s", unmet)
	}
	gotConfig, _ := os.ReadFile(configPath)
	gotLock, _ := os.ReadFile(lockPath)
	if string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("status rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, factoryDir)) {
		t.Fatal("status changed materialized bytes")
	}
	requireIdenticalBytes(t, helperPath, helperBytes, "materialized helper SKILL.md")
}

// spec: resource-prerequisites/doctor-explains-an-unconfigured-hint-without-mutation
func TestSurfaceRequirementsDoctorReportsUnconfiguredHint(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, origin, _ := writeSurfaceRegistry(t)
	surfaceRepoConfig(t, repoRoot, origin, "skill/hinter, skill/provider, skill/needy, skill/unrelated, skill/root", true)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("install", "hinter", "provider", "needy", "root"); err != nil {
		t.Fatalf("install fixture: %v\n%s", err, output)
	}

	factoryDir := filepath.Join(repoRoot, ".factory")
	snapshot := ownedCommandSnapshot(t, factoryDir)
	materialized := map[string][]byte{}
	for _, path := range []string{
		filepath.Join(factoryDir, "skills", "helper", "SKILL.md"),
		filepath.Join(factoryDir, "skills", "root", "SKILL.md"),
		filepath.Join(factoryDir, "mcp.json"),
	} {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read materialized %s: %v", path, readErr)
		}
		materialized[path] = raw
	}
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, output)
	}
	if !strings.Contains(output, "unconfigured prerequisite source") {
		t.Fatalf("doctor did not explain the unconfigured hint:\n%s", output)
	}
	if !strings.Contains(output, `requires skill "ghost"`) {
		t.Fatalf("doctor lost the real unmet hinted requirement:\n%s", output)
	}
	if !strings.Contains(output, "https://***@example.invalid/tools.git") {
		t.Fatalf("doctor dropped the redacted hint:\n%s", output)
	}
	if strings.Contains(output, surfaceCredHint) || strings.Contains(output, "s3cr3t") {
		t.Fatalf("credential-bearing hint leaked:\n%s", output)
	}
	if strings.Contains(output, `requires skill "provider"`) {
		t.Fatalf("doctor accused a satisfied hinted provider:\n%s", output)
	}
	for _, satisfied := range []string{`requires skill "helper"`, `requires mcp "docs"`} {
		if strings.Contains(output, satisfied) {
			t.Fatalf("doctor accused a satisfied prerequisite %q:\n%s", satisfied, output)
		}
	}
	gotConfig, _ := os.ReadFile(configPath)
	gotLock, _ := os.ReadFile(lockPath)
	if string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("doctor rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, factoryDir)) {
		t.Fatal("doctor changed materialized bytes")
	}
	for path, want := range materialized {
		requireIdenticalBytes(t, path, want, path)
	}
}

// spec: resource-prerequisites/doctor-does-not-accuse-satisfied-or-pending-providers
func TestSurfaceRequirementsDoctorPendingMarketplaceProvider(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	repoRoot := t.TempDir()
	origin, working := createBareRepoWithWorkingClone(t, "pending-registry.git")
	if err := os.MkdirAll(filepath.Join(working, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillRequiringItems(t, working, "reviewer", "Review dependent", []any{map[string]string{"kind": "skill", "name": "review", "registry": surfaceCredHint}, "skill:elsewhere"})
	commitAndPushAll(t, working, "pending fixtures")
	configPath := surfaceRepoConfig(t, repoRoot, origin, "skill/reviewer", true)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("install", "reviewer"); err != nil {
		t.Fatalf("install reviewer fixture: %v\n%s", err, output)
	}
	server, downloads := newCountingV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	if output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace provider: %v\n%s", err, output)
	}
	if err := os.RemoveAll(filepath.Join(cache.CacheDir(), "marketplace", "artifacts")); err != nil {
		t.Fatalf("remove cached Marketplace artifacts: %v", err)
	}
	server.Close()
	downloadsBefore := downloads.Load()
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	userConfigBytes, err := os.ReadFile(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ownedCommandSnapshot(t, filepath.Join(repoRoot, ".factory"))

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, output)
	}
	if strings.Contains(output, `requires skill "review"`) {
		t.Fatalf("doctor accused a pending Marketplace provider:\n%s", output)
	}
	if strings.Contains(output, "unconfigured prerequisite source") {
		t.Fatalf("doctor accused the pending provider's hinted source:\n%s", output)
	}
	if !strings.Contains(output, `requires skill "elsewhere"`) {
		t.Fatalf("doctor lost the real unmet warning:\n%s", output)
	}
	if strings.Contains(output, "s3cr3t") {
		t.Fatalf("credential-bearing hint leaked:\n%s", output)
	}
	if got := downloads.Load(); got != downloadsBefore {
		t.Fatalf("doctor triggered %d artifact downloads", got-downloadsBefore)
	}
	gotUserConfig, _ := os.ReadFile(userConfigPath)
	gotConfig, _ := os.ReadFile(configPath)
	gotLock, _ := os.ReadFile(lockPath)
	if string(gotUserConfig) != string(userConfigBytes) || string(gotConfig) != string(configBytes) || string(gotLock) != string(lockBytes) {
		t.Fatal("doctor rewrote config or lock")
	}
	if !reflect.DeepEqual(snapshot, ownedCommandSnapshot(t, filepath.Join(repoRoot, ".factory"))) {
		t.Fatal("doctor changed materialized bytes")
	}
}
