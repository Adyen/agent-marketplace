package e2e_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- declarative test types ----------

// TestCase defines a single E2E test scenario declaratively.
type TestCase struct {
	Name         string           // subtest name (e.g. "collision/same_name_different_registries")
	Desc         string           // Given/When/Then description of the test scenario
	Registries   []RegistryDef    // bare repos to seed inside the container
	Marketplaces []MarketplaceDef // in-process Marketplace servers and their source repositories
	GlobalCfg    string           // ~/.agents/config.yaml content; placeholders resolved; always written (empty → explicit v4 fixture defaults)
	RepoCfg      string           // .agents/config.yaml content; omitted if empty
	LocalCfg     string           // .agents/config.local.yaml content; omitted if empty
	OrgCfg       string           // /etc/agent-marketplace/config.yaml content; omitted if empty
	Steps        []Step           // sequence of actions and assertions
}

// RegistryDef describes a bare git repo to create inside the container.
// Either use Skills/Commands/Subagents/MCPServers for standard layouts,
// or Files for arbitrary file trees (path -> content).
type RegistryDef struct {
	Name       string
	Skills     []Skill
	Commands   []Command
	Subagents  []Subagent
	MCPServers []MCPServer
	Files      map[string]string // raw files; overrides structured fields if set
}

// FilesMap returns the map[string]string suitable for CreateBareRepo.
// Structured resources (Skills, Commands, Subagents, MCPServers) are always
// included; raw Files entries overlay on top so COLLECTION.yaml and custom
// files coexist with auto-generated resource stubs.
func (r RegistryDef) FilesMap() map[string]string {
	files := RegistryFilesEx(r.Skills, r.Commands, r.Subagents, r.MCPServers)
	for p, c := range r.Files {
		files[p] = c
	}
	return files
}

// MarketplaceDef describes an in-process Marketplace backed by a bare source repository.
type MarketplaceDef struct {
	Name     string
	Registry RegistryDef
}

// StepAction enumerates the side-effect actions a Step can perform before
// running a command (or without running a command at all).
type StepAction int

const (
	ActionNone StepAction = iota
	ActionWriteGlobalConfig
	ActionWriteRepoConfig
	ActionWriteLocalConfig
	ActionWriteOrgConfig
	ActionWriteFile
	ActionPushRegistry
	ActionReindexMarketplace
	ActionRevokeMarketplace
)

type MarketplaceRevocations struct {
	Coordinates []string
	Revisions   []string
}

// Step is a single operation within a test case. A step either runs an
// agent-marketplace command (Args set) or performs a side-effect action
// (Action set), or both (action first, then command). Assertions are
// checked after the command runs.
type Step struct {
	// Action is a side-effect to perform before running Args.
	Action StepAction
	// Body is the content for Action writes (config content, file content).
	Body string
	// Path is the container path for ActionWriteFile.
	Path string
	// Target names a Registry or Marketplace for a Marketplace action.
	Target string
	// Files are source repository paths and contents for ActionPushRegistry.
	Files map[string]string
	// Revoke declares Marketplace coordinates and/or revisions to revoke.
	Revoke MarketplaceRevocations
	// Env overrides subprocess environment values.
	Env map[string]string
	// Cwd selects the subprocess working directory.
	Cwd string

	// Args are the arguments to agent-marketplace. If empty and Action is set,
	// the step only performs the action without running a command.
	Args []string

	// AssertExitCode checks the exit code of the command (default: not checked).
	// Only effective when Args is set.
	AssertExitCode *int
	// MustPass is a shorthand: if true, AssertExitCode is expected to be 0
	// and the test fails immediately on mismatch (no subsequent assertions run).
	MustPass bool

	// Output substring assertions (only effective when Args is set).
	OutContains    []string
	OutNotContains []string

	// Filesystem assertions (run after the step's command/action).
	FilesExist      []string
	FilesAbsent     []string
	PathsUnchanged  []string // files or trees whose bytes and metadata must be unchanged by this step
	Symlinks        []string
	RegularFiles    []string          // paths that must exist and not be symbolic links
	FileContains    map[string]string // path -> substring
	FileNotContains map[string]string // path -> substring that must NOT appear
}

// ---------- placeholder resolution ----------

func resolveTableString(t *testing.T, value string, tokens map[string]string) string {
	t.Helper()
	for token, replacement := range tokens {
		value = strings.ReplaceAll(value, token, replacement)
	}
	if token := unresolvedTableToken(value); token != "" {
		t.Fatalf("unresolved table placeholder %q", token)
	}
	return value
}

func unresolvedTableToken(value string) string {
	for _, prefix := range []string{"{{MKT:", "{{GITHTTP:"} {
		if start := strings.Index(value, prefix); start >= 0 {
			end := strings.Index(value[start:], "}}")
			if end >= 0 {
				return value[start : start+end+2]
			}
			return value[start:]
		}
	}
	return ""
}

func resolveTableCase(t *testing.T, tc TestCase, tokens map[string]string) TestCase {
	t.Helper()
	tc = expandTablePathShorthand(tc)
	resolve := func(value string) string { return resolveTableString(t, value, tokens) }
	tc.GlobalCfg = resolve(tc.GlobalCfg)
	tc.RepoCfg = resolve(tc.RepoCfg)
	tc.LocalCfg = resolve(tc.LocalCfg)
	tc.OrgCfg = resolve(tc.OrgCfg)
	tc.Steps = append([]Step(nil), tc.Steps...)
	for i := range tc.Steps {
		step := &tc.Steps[i]
		step.Body = resolve(step.Body)
		step.Path = resolve(step.Path)
		step.Cwd = resolve(step.Cwd)
		step.Args = resolveTableStrings(t, step.Args, tokens)
		step.OutContains = resolveTableStrings(t, step.OutContains, tokens)
		step.OutNotContains = resolveTableStrings(t, step.OutNotContains, tokens)
		step.FilesExist = resolveTableStrings(t, step.FilesExist, tokens)
		step.FilesAbsent = resolveTableStrings(t, step.FilesAbsent, tokens)
		step.PathsUnchanged = resolveTableStrings(t, step.PathsUnchanged, tokens)
		step.Symlinks = resolveTableStrings(t, step.Symlinks, tokens)
		step.RegularFiles = resolveTableStrings(t, step.RegularFiles, tokens)
		step.Files = resolveTableMap(t, step.Files, tokens)
		step.Env = resolveTableMap(t, step.Env, tokens)
		step.Revoke.Coordinates = resolveTableStrings(t, step.Revoke.Coordinates, tokens)
		step.Revoke.Revisions = resolveTableStrings(t, step.Revoke.Revisions, tokens)
		step.FileContains = resolveTableMap(t, step.FileContains, tokens)
		step.FileNotContains = resolveTableMap(t, step.FileNotContains, tokens)
	}
	return tc
}

func resolveTableStrings(t *testing.T, values []string, tokens map[string]string) []string {
	t.Helper()
	resolved := make([]string, len(values))
	for i, value := range values {
		resolved[i] = resolveTableString(t, value, tokens)
	}
	return resolved
}

func resolveTableMap(t *testing.T, values map[string]string, tokens map[string]string) map[string]string {
	t.Helper()
	if values == nil {
		return nil
	}
	resolved := make(map[string]string, len(values))
	for key, value := range values {
		resolved[resolveTableString(t, key, tokens)] = resolveTableString(t, value, tokens)
	}
	return resolved
}

func tableMarketplaceRegistry(skills ...Skill) RegistryDef {
	files := make(map[string]string, len(skills))
	for _, skill := range skills {
		content := strings.Replace(skill.SkillFile(), "---\n", "---\nlicense: MIT\n", 1)
		files["skills/"+skill.Name+"/SKILL.md"] = content
	}
	return RegistryDef{Files: files}
}

func pathFingerprint(root string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%q\x00%s\x00%d\x00%d\x00", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = h.Write([]byte(target))
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = h.Write(data)
		}
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func TestPathFingerprintDetectsSameByteRewrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "artifact")
	if err := os.WriteFile(path, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstTime := time.Unix(1, 0)
	if err := os.Chtimes(path, firstTime, firstTime); err != nil {
		t.Fatal(err)
	}
	before, err := pathFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondTime := time.Unix(2, 0)
	if err := os.Chtimes(path, secondTime, secondTime); err != nil {
		t.Fatal(err)
	}
	after, err := pathFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("path fingerprint ignored a same-byte rewrite")
	}
}

func TestTableCaseValidationAndUnresolvedPlaceholder(t *testing.T) {
	tests := []struct {
		name           string
		testCase       TestCase
		value          string
		wantValidation bool
		wantUnresolved string
	}{
		{
			name: "valid targets and REG placeholder",
			testCase: TestCase{
				Registries:   []RegistryDef{{Name: "registry"}},
				Marketplaces: []MarketplaceDef{{Name: "marketplace"}},
				Steps: []Step{
					{Action: ActionPushRegistry, Target: "registry"},
					{Action: ActionReindexMarketplace, Target: "marketplace"},
				},
			},
			value: "{{REG:x}}",
		},
		{
			name:           "unknown target",
			testCase:       TestCase{Steps: []Step{{Action: ActionPushRegistry, Target: "missing"}}},
			wantValidation: true,
		},
		{
			name:           "unknown Marketplace target",
			testCase:       TestCase{Steps: []Step{{Action: ActionReindexMarketplace, Target: "missing"}}},
			wantValidation: true,
		},
		{
			name: "registry and marketplace share a name",
			testCase: TestCase{
				Registries:   []RegistryDef{{Name: "shared"}},
				Marketplaces: []MarketplaceDef{{Name: "shared"}},
			},
			wantValidation: true,
		},
		{
			name:           "step sets Env and Cwd",
			testCase:       TestCase{Steps: []Step{{Env: map[string]string{"KEY": "value"}, Cwd: "/workspace"}}},
			wantValidation: true,
		},
		{
			name:           "unresolved Marketplace token",
			value:          "prefix {{MKT:missing}} suffix",
			wantUnresolved: "{{MKT:missing}}",
		},
		{
			name:           "unresolved Git HTTP token",
			value:          "{{GITHTTP:missing}}",
			wantUnresolved: "{{GITHTTP:missing}}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTableCase(test.testCase); (err != nil) != test.wantValidation {
				t.Fatalf("validateTableCase() error = %v, wantError %t", err, test.wantValidation)
			}

			if got := unresolvedTableToken(test.value); got != test.wantUnresolved {
				t.Fatalf("unresolvedTableToken(%q) = %q, want %q", test.value, got, test.wantUnresolved)
			}
			if test.wantUnresolved == "" {
				if got := resolveTableString(t, test.value, nil); got != test.value {
					t.Fatalf("resolveTableString(%q) = %q, want unchanged value", test.value, got)
				}
			}
		})
	}
}

const handwrittenFactoryHooks = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo handwritten"}]}]}}`

var testTable []TestCase

// v4 returns a v4 config with the given lines after the apiVersion header.
func v4(lines ...string) string {
	cfg := "apiVersion: agent-marketplace/v4\n"
	for _, line := range lines {
		cfg += line + "\n"
	}
	return cfg
}

// run runs agent-marketplace without asserting its exit code.
func run(args ...string) Step { return Step{Args: args} }

// ok runs agent-marketplace and requires exit code 0.
func ok(args ...string) Step { return Step{Args: args, MustPass: true} }

// exits runs agent-marketplace and requires the given exit code.
func exits(code int, args ...string) Step { return Step{Args: args, AssertExitCode: intPtr(code)} }

// writeFile writes body to the path before the step runs.
func writeFile(path, body string) Step { return Step{Action: ActionWriteFile, Path: path, Body: body} }

// writeGlobalCfg writes the user-wide config before the step runs.
func writeGlobalCfg(body string) Step { return Step{Action: ActionWriteGlobalConfig, Body: body} }

// writeRepoCfg writes the repository config before the step runs.
func writeRepoCfg(body string) Step { return Step{Action: ActionWriteRepoConfig, Body: body} }

// writeLocalCfg writes the repository-local config before the step runs.
func writeLocalCfg(body string) Step { return Step{Action: ActionWriteLocalConfig, Body: body} }

// writeOrgCfg writes the organization config before the step runs.
func writeOrgCfg(body string) Step { return Step{Action: ActionWriteOrgConfig, Body: body} }

// push publishes files to the named registry before the step runs.
func push(target string, files map[string]string) Step {
	return Step{Action: ActionPushRegistry, Target: target, Files: files}
}

// reindex rebuilds the named Marketplace before the step runs.
func reindex(target string) Step { return Step{Action: ActionReindexMarketplace, Target: target} }

// revoke revokes the given coordinates and revisions on the named Marketplace before the step runs.
func revoke(target string, revocations MarketplaceRevocations) Step {
	return Step{Action: ActionRevokeMarketplace, Target: target, Revoke: revocations}
}

// Chainable Step modifiers. Each returns a modified copy of the step.

func (s Step) then(args ...string) Step { s.Args = args; return s }

func (s Step) pass() Step { s.MustPass = true; return s }

func (s Step) exit(code int) Step { s.AssertExitCode = intPtr(code); return s }

func (s Step) in(cwd string) Step { s.Cwd = cwd; return s }

func (s Step) env(key, value string) Step { s.Env = withEntry(s.Env, key, value); return s }

func (s Step) out(substrings ...string) Step {
	s.OutContains = withStrings(s.OutContains, substrings...)
	return s
}

func (s Step) notOut(substrings ...string) Step {
	s.OutNotContains = withStrings(s.OutNotContains, substrings...)
	return s
}

func (s Step) exists(paths ...string) Step {
	s.FilesExist = withStrings(s.FilesExist, paths...)
	return s
}

func (s Step) absent(paths ...string) Step {
	s.FilesAbsent = withStrings(s.FilesAbsent, paths...)
	return s
}

func (s Step) unchanged(paths ...string) Step {
	s.PathsUnchanged = withStrings(s.PathsUnchanged, paths...)
	return s
}

func (s Step) symlinks(paths ...string) Step {
	s.Symlinks = withStrings(s.Symlinks, paths...)
	return s
}

func (s Step) regular(paths ...string) Step {
	s.RegularFiles = withStrings(s.RegularFiles, paths...)
	return s
}

func (s Step) has(path, substring string) Step {
	s.FileContains = withEntry(s.FileContains, path, substring)
	return s
}

func (s Step) lacks(path, substring string) Step {
	s.FileNotContains = withEntry(s.FileNotContains, path, substring)
	return s
}

func withStrings(list []string, more ...string) []string {
	return append(append([]string(nil), list...), more...)
}

func withEntry(entries map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(entries)+1)
	for k, v := range entries {
		out[k] = v
	}
	out[key] = value
	return out
}

// expandTablePathShorthand rewrites path-typed fields: "./" means "/workspace/" and "~/" means "/root/".
func expandTablePathShorthand(tc TestCase) TestCase {
	if tc.Steps == nil {
		return tc
	}
	steps := make([]Step, len(tc.Steps))
	for i, step := range tc.Steps {
		step.Path = expandTablePath(step.Path)
		step.Cwd = expandTablePath(step.Cwd)
		step.FilesExist = expandTablePaths(step.FilesExist)
		step.FilesAbsent = expandTablePaths(step.FilesAbsent)
		step.PathsUnchanged = expandTablePaths(step.PathsUnchanged)
		step.Symlinks = expandTablePaths(step.Symlinks)
		step.RegularFiles = expandTablePaths(step.RegularFiles)
		step.FileContains = expandTableMapKeys(step.FileContains)
		step.FileNotContains = expandTableMapKeys(step.FileNotContains)
		steps[i] = step
	}
	tc.Steps = steps
	return tc
}

func expandTablePath(path string) string {
	switch {
	case strings.HasPrefix(path, "./"):
		return "/workspace/" + path[len("./"):]
	case strings.HasPrefix(path, "~/"):
		return "/root/" + path[len("~/"):]
	}
	return path
}

func expandTablePaths(paths []string) []string {
	if paths == nil {
		return nil
	}
	expanded := make([]string, len(paths))
	for i, path := range paths {
		expanded[i] = expandTablePath(path)
	}
	return expanded
}

func expandTableMapKeys(entries map[string]string) map[string]string {
	if entries == nil {
		return nil
	}
	expanded := make(map[string]string, len(entries))
	for path, value := range entries {
		expanded[expandTablePath(path)] = value
	}
	return expanded
}

func TestResolveTableCaseExpandsPathShorthand(t *testing.T) {
	tc := TestCase{Steps: []Step{{
		Path:         "./a",
		Cwd:          "~/b",
		FilesExist:   []string{"./c", "/workspace/d"},
		FileContains: map[string]string{"~/e": "x"},
		Args:         []string{"./resources"},
		Body:         "./unchanged",
	}}}
	got := resolveTableCase(t, tc, nil).Steps[0]
	if got.Path != "/workspace/a" || got.Cwd != "/root/b" || got.Body != "./unchanged" ||
		!reflect.DeepEqual(got.FilesExist, []string{"/workspace/c", "/workspace/d"}) ||
		!reflect.DeepEqual(got.FileContains, map[string]string{"/root/e": "x"}) ||
		!reflect.DeepEqual(got.Args, []string{"./resources"}) {
		t.Fatalf("resolveTableCase() step = %+v", got)
	}
}

func TestTableCaseNamesAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(testTable))
	for _, tc := range testTable {
		if seen[tc.Name] {
			t.Errorf("duplicate table case name %q", tc.Name)
		}
		seen[tc.Name] = true
	}
}

func TestStepBuildersMatchLiterals(t *testing.T) {
	got := ok("sync").out("a").exists("./x").has("./y", "z")
	want := Step{Args: []string{"sync"}, MustPass: true, OutContains: []string{"a"}, FilesExist: []string{"./x"}, FileContains: map[string]string{"./y": "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("builder step = %+v, want %+v", got, want)
	}
}

// ---------- parallelism configuration ----------

// e2eParallelism returns the maximum number of concurrent test containers.
// Controlled by E2E_PARALLEL env var; defaults to 4. Set to 1 for serial.
func e2eParallelism() int {
	if v := os.Getenv("E2E_PARALLEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return 4
}

// ---------- helper ----------

func intPtr(v int) *int { return &v }

// ---------- test report ----------

// TestResult captures the outcome of a single test case.
type TestResult struct {
	Name       string `json:"name"`
	Desc       string `json:"description"`
	Status     string `json:"status"` // "pass", "fail", "skip"
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// TestReport captures the full E2E suite outcome.
type TestReport struct {
	Timestamp  time.Time    `json:"timestamp"`
	Total      int          `json:"total"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped"`
	DurationMs int64        `json:"duration_ms"`
	Results    []TestResult `json:"results"`
}

// reportWriter is a goroutine-safe collector for test results.
type reportWriter struct {
	mu      sync.Mutex
	results []TestResult
}

func (rw *reportWriter) add(r TestResult) {
	rw.mu.Lock()
	rw.results = append(rw.results, r)
	rw.mu.Unlock()
}

func (rw *reportWriter) write(reportPath string, suiteStart time.Time) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	report := TestReport{
		Timestamp:  time.Now().UTC(),
		Total:      len(rw.results),
		DurationMs: time.Since(suiteStart).Milliseconds(),
		Results:    rw.results,
	}
	for _, r := range rw.results {
		switch r.Status {
		case "pass":
			report.Passed++
		case "fail":
			report.Failed++
		case "skip":
			report.Skipped++
		}
	}

	// Write JSON report.
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e report: marshal: %v\n", err)
		return
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "e2e report: write: %v\n", err)
	}

	// Print summary to stdout.
	fmt.Println("\n========== E2E Test Report ==========")
	fmt.Printf("  Total:   %d\n", report.Total)
	fmt.Printf("  Passed:  %d\n", report.Passed)
	fmt.Printf("  Failed:  %d\n", report.Failed)
	fmt.Printf("  Skipped: %d\n", report.Skipped)
	fmt.Printf("  Duration: %s\n", time.Duration(report.DurationMs*int64(time.Millisecond)).Round(time.Second))
	fmt.Println("======================================")
	if report.Failed > 0 {
		fmt.Println("\nFailed tests:")
		for _, r := range report.Results {
			if r.Status == "fail" {
				fmt.Printf("  - %s (%dms)\n", r.Name, r.DurationMs)
				if r.Desc != "" {
					fmt.Printf("    %s\n", r.Desc)
				}
				if r.Error != "" {
					firstLine := strings.SplitN(r.Error, "\n", 2)[0]
					fmt.Printf("    %s\n", firstLine)
				}
			}
		}
	}
	fmt.Printf("\nFull report: %s\n", reportPath)
}

// ---------- table-driven test runner ----------

// TestE2E runs the full declarative test table in isolated temp directories.
// By default up to 4 tests run in parallel (each in its own sandbox);
// set E2E_PARALLEL=1 for serial execution, or any positive integer for
// a different concurrency limit.
func TestE2E(t *testing.T) {
	reportPath := os.Getenv("E2E_REPORT")
	if reportPath == "" {
		reportPath = "e2e-report.json"
	}

	parallelism := e2eParallelism()
	// Token-pool semaphore: a buffered channel pre-filled with `parallelism`
	// tokens. Each parallel test receives a token to acquire and sends it
	// back to release. This avoids the deadlock that occurs when t.Parallel()
	// resumes all subtests at once and they race to send into an empty
	// channel — with a token pool, receiving blocks (correctly) until a
	// token is available, and the first `parallelism` tests proceed
	// immediately.
	sem := make(chan struct{}, parallelism)
	for i := 0; i < parallelism; i++ {
		sem <- struct{}{}
	}

	suiteStart := time.Now()
	rw := &reportWriter{}

	// Ensure report is written even if tests are interrupted.
	t.Cleanup(func() {
		rw.write(reportPath, suiteStart)
	})

	t.Logf("parallelism: %d (set E2E_PARALLEL to change)", parallelism)

	for _, tc := range testTable {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			// Acquire a token — blocks if `parallelism` containers are
			// already running. Released via defer after test body completes.
			<-sem
			defer func() { sem <- struct{}{} }()

			start := time.Now()

			// Record result for report via defer so fatal failures are still captured.
			var firstErr string
			defer func() {
				status := "pass"
				if t.Failed() {
					status = "fail"
				}
				rw.add(TestResult{
					Name:       tc.Name,
					Desc:       tc.Desc,
					Status:     status,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      firstErr,
				})
			}()
			runTableCase(t, tc, &firstErr)
		})
	}
}

func runTableCase(t *testing.T, tc TestCase, firstErr *string) {
	t.Helper()
	if err := validateTableCase(tc); err != nil {
		t.Fatalf("%s", err)
	}
	originalSteps := tc.Steps
	env := newEnv(t)
	env.InitGitRepo()

	// Seed registries and collect their fixture URLs.
	bareURLs := make(map[string]string, len(tc.Registries)+len(tc.Marketplaces))
	for _, registry := range tc.Registries {
		bareURLs[registry.Name] = env.CreateBareRepo(registry.Name, registry.FilesMap())
	}
	var gitServerURL string
	if len(bareURLs)+len(tc.Marketplaces) > 0 {
		gitServer := newGitHTTPServer(t, env.repos)
		gitServerURL = gitServer.URL
		t.Cleanup(gitServer.Close)
	}

	tokens := make(map[string]string, 2*len(bareURLs)+2*len(tc.Marketplaces))
	for name, url := range bareURLs {
		tokens["{{REG:"+name+"}}"] = url
		tokens["{{GITHTTP:"+name+"}}"] = gitServerURL + "/" + name + ".git"
	}
	marketplaces := make(map[string]*marketplaceTarget, len(tc.Marketplaces))
	for _, definition := range tc.Marketplaces {
		target := newMarketplaceTarget(t, env, definition.Name, definition.Registry, gitServerURL,
			definition.Name+"-marketplace", definition.Name, t.TempDir())
		marketplaces[definition.Name] = target
		bareURLs[definition.Name] = target.bareURL
		tokens["{{GITHTTP:"+definition.Name+"}}"] = target.origin
		tokens["{{MKT:"+definition.Name+"}}"] = target.server.URL
	}
	tc = resolveTableCase(t, tc, tokens)

	// Write authored config layers after resolving fixture URLs.
	// Cases that omit the user fixture get an explicit v4 skeleton; no
	// authored legacy installation text is translated by setup.
	globalCfg := tc.GlobalCfg
	if globalCfg == "" {
		globalCfg = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	}
	env.WriteGlobalConfig(globalCfg)
	if tc.RepoCfg != "" {
		env.WriteRepoConfig(tc.RepoCfg)
	}
	if tc.LocalCfg != "" {
		env.WriteLocalConfig(tc.LocalCfg)
	}
	if tc.OrgCfg != "" {
		env.WriteOrgConfig(tc.OrgCfg)
	}

	for i, step := range tc.Steps {
		// Perform side-effect action first.
		switch step.Action {
		case ActionWriteGlobalConfig:
			env.WriteGlobalConfig(step.Body)
		case ActionWriteRepoConfig:
			env.WriteRepoConfig(step.Body)
		case ActionWriteLocalConfig:
			env.WriteLocalConfig(step.Body)
		case ActionWriteOrgConfig:
			env.WriteOrgConfig(step.Body)
		case ActionWriteFile:
			env.WriteFile(step.Path, step.Body)
		case ActionPushRegistry:
			pushRegistryFiles(t, bareURLs[step.Target], step.Files)
		case ActionReindexMarketplace:
			marketplaces[step.Target].reindex(t, marketplaces[step.Target].cfg)
		case ActionRevokeMarketplace:
			marketplaces[step.Target].revoke(t, step.Revoke)
		}

		unchanged := make(map[string]string, len(step.PathsUnchanged))
		for _, path := range step.PathsUnchanged {
			fingerprint, err := pathFingerprint(env.mustResolvePath(path))
			if err != nil {
				tableStepError(t, firstErr, fmt.Sprintf("step %d: snapshot path %q: %v", i, path, err), "", true)
			}
			unchanged[path] = fingerprint
		}

		var result ExecResult
		if len(step.Args) > 0 {
			switch {
			case step.Env != nil:
				result = env.RunAMWithEnv(step.Env, step.Args...)
			case step.Cwd != "":
				result = env.RunAMFrom(step.Cwd, step.Args...)
			default:
				result = env.RunAM(step.Args...)
			}
			if step.MustPass && result.ExitCode != 0 {
				tableStepError(t, firstErr, fmt.Sprintf("step %d: agent-marketplace %s failed (exit %d): %s",
					i, strings.Join(originalSteps[i].Args, " "), result.ExitCode, result.Combined), "", true)
			}
			if step.AssertExitCode != nil && result.ExitCode != *step.AssertExitCode {
				tableStepError(t, firstErr, fmt.Sprintf("step %d: exit code got %d want %d", i, result.ExitCode, *step.AssertExitCode), "output:\n"+result.Combined, false)
			}
			for _, text := range step.OutContains {
				if !strings.Contains(result.Combined, text) {
					tableStepError(t, firstErr, fmt.Sprintf("step %d: output missing %q", i, text), "full output:\n"+result.Combined, false)
				}
			}
			for _, text := range step.OutNotContains {
				if strings.Contains(result.Combined, text) {
					tableStepError(t, firstErr, fmt.Sprintf("step %d: output unexpectedly contains %q", i, text), "full output:\n"+result.Combined, false)
				}
			}
		}
		assertTableStep(t, env, step, unchanged, firstErr, i)
	}
}

func validateTableCase(tc TestCase) error {
	names := make(map[string]string, len(tc.Registries)+len(tc.Marketplaces))
	for _, registry := range tc.Registries {
		if prior, ok := names[registry.Name]; ok {
			return fmt.Errorf("test case %q has duplicate target name %q (%s and registry)", tc.Name, registry.Name, prior)
		}
		names[registry.Name] = "registry"
	}
	marketplaceNames := make(map[string]bool, len(tc.Marketplaces))
	for _, marketplace := range tc.Marketplaces {
		if prior, ok := names[marketplace.Name]; ok {
			return fmt.Errorf("test case %q shares target name %q between %s and Marketplace", tc.Name, marketplace.Name, prior)
		}
		names[marketplace.Name] = "Marketplace"
		marketplaceNames[marketplace.Name] = true
	}
	for i, step := range tc.Steps {
		if step.Env != nil && step.Cwd != "" {
			return fmt.Errorf("test case %q step %d sets both Env and Cwd", tc.Name, i)
		}
		switch step.Action {
		case ActionPushRegistry:
			if names[step.Target] == "" {
				return fmt.Errorf("test case %q step %d has unknown Target %q", tc.Name, i, step.Target)
			}
		case ActionReindexMarketplace, ActionRevokeMarketplace:
			if !marketplaceNames[step.Target] {
				return fmt.Errorf("test case %q step %d has unknown Marketplace Target %q", tc.Name, i, step.Target)
			}
		}
	}
	return nil
}

func tableStepError(t *testing.T, firstErr *string, message, detail string, fatal bool) {
	t.Helper()
	if *firstErr == "" {
		*firstErr = message
	}
	if fatal {
		t.Fatalf("%s", message)
	}
	if detail != "" {
		t.Errorf("%s\n%s", message, detail)
		return
	}
	t.Errorf("%s", message)
}

func assertTableStep(t *testing.T, env *Env, step Step, unchanged map[string]string, firstErr *string, index int) {
	t.Helper()
	for _, path := range step.FilesExist {
		if !env.CheckFileExists(path) {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: file %q does not exist", index, path), "", false)
		}
	}
	for _, path := range step.FilesAbsent {
		absent, err := checkPathAbsent(env.mustResolvePath(path))
		if !absent || err != nil {
			message := fmt.Sprintf("step %d: file %q unexpectedly exists", index, path)
			if err != nil {
				message = fmt.Sprintf("step %d: cannot inspect %q: %v", index, path, err)
			}
			tableStepError(t, firstErr, message, "", false)
		}
	}
	for path, before := range unchanged {
		after, err := pathFingerprint(env.mustResolvePath(path))
		if err != nil {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: unchanged path %q is not readable: %v", index, path, err), "", false)
			continue
		}
		if after != before {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: path %q changed", index, path), "", false)
		}
	}
	for _, path := range step.Symlinks {
		if !env.CheckSymlink(path) {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: path %q is not a symlink", index, path), "", false)
		}
	}
	for _, path := range step.RegularFiles {
		if !env.CheckRegularFile(path) {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: path %q is not a regular file", index, path), "", false)
		}
	}
	for path, text := range step.FileContains {
		if !env.CheckFileContains(path, text) {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: file %q does not contain %q", index, path, text), "", false)
		}
	}
	for path, text := range step.FileNotContains {
		if env.CheckFileContains(path, text) {
			tableStepError(t, firstErr, fmt.Sprintf("step %d: file %q unexpectedly contains %q", index, path, text), "", false)
		}
	}
}
