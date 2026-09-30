package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEngine(t *testing.T, res []Resource) (*Engine, *Snapshot) {
	t.Helper()
	snap := &Snapshot{Resources: res}
	store := &SnapshotStore{}
	store.Store(snap)
	e := &Engine{
		Snapshots: store,
		Skills:    fakeSkills{},
		Pool:      newFakePool(),
		Log:       NewSessionLog(t.TempDir(), "test-session"),
	}
	return e, snap
}

type fakeSkills struct{}

func (fakeSkills) Load(ctx context.Context, res *Resource) (SkillContent, error) {
	return SkillContent{Ref: res.Ref, Revision: res.Revision, Digest: "sha256:abc", Content: "# " + res.Name, Files: []string{"a.txt"}}, nil
}

func (fakeSkills) ReadFile(ctx context.Context, res *Resource, path string) (string, error) {
	if path == "secret" {
		return "", errors.New("denied")
	}
	return "file:" + path, nil
}

type fakePool struct {
	started []string
}

func newFakePool() *fakePool { return &fakePool{} }

func (p *fakePool) Tools(ctx context.Context, alias string, res *Resource) ([]ToolSchema, error) {
	p.started = append(p.started, alias)
	return []ToolSchema{{Name: "ping"}}, nil
}

func (p *fakePool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	if tool == "fail" {
		return nil, CodeError(ErrCodeToolError, "boom")
	}
	return map[string]any{"tool": tool, "alias": alias}, nil
}

func (p *fakePool) Close() error { return nil }

// spec: code-mode-mcp/search-binds-only-the-catalog
func TestSearchCatalogBinding(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/skill/pdf", Kind: "skill", Name: "pdf", Registry: "reg", Revision: "main", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{"code": `async () => catalog.filter(r => r.kind === "skill").map(r => r.ref)`})
	res, _, err := e.Search(context.Background(), args)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if string(res.Value) != `["reg/skill/pdf"]` {
		t.Fatalf("value = %s", res.Value)
	}
}

// spec: code-mode-mcp/search-never-starts-servers
func TestSearchNeverStartsServers(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Revision: "main", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{"code": `async () => typeof mcp === 'undefined'`})
	res, _, err := e.Search(context.Background(), args)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if string(res.Value) != `true` {
		t.Fatalf("mcp binding leaked into search: %s", res.Value)
	}
}

// spec: code-mode-mcp/unknown-or-malformed-refs-rejected
func TestExecuteGatesUnknownRef(t *testing.T) {
	e, _ := testEngine(t, nil)
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/missing"},
		"code":    `async () => 1`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeNotFound {
		t.Fatalf("expected %s, got %v", ErrCodeNotFound, err)
	}
}

// spec: code-mode-mcp/declared-refs-must-be-catalog-mcp-servers
func TestExecuteGatesNonMCPKind(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/skill/pdf", Kind: "skill", Name: "pdf", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/skill/pdf"},
		"code":    `async () => 1`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeUnsupportedKind {
		t.Fatalf("expected %s, got %v", ErrCodeUnsupportedKind, err)
	}
}

// spec: code-mode-mcp/undeclared-alias-rejected
func TestExecuteUndeclaredAlias(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => mcp.other.ping()`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeNotDeclared {
		t.Fatalf("expected %s, got %v", ErrCodeNotDeclared, err)
	}
}

func TestExecuteToolCallCatchable(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => { try { await mcp.srv.fail() } catch (e) { return "caught" } return "missed" }`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `"caught"` {
		t.Fatalf("value = %s", res.Value)
	}
}

// spec: code-mode-mcp/skills-load-and-read-on-demand
func TestExecuteSkillLoadAndLog(t *testing.T) {
	dir := t.TempDir()
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/skill/pdf", Kind: "skill", Name: "pdf", Registry: "reg", Revision: "v1", Assessment: "allow"},
	})
	e.Log = NewSessionLog(dir, "sess-1")
	args, _ := json.Marshal(map[string]any{
		"code": `async () => (await skills.load("reg/skill/pdf")).content`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `"# pdf"` {
		t.Fatalf("value = %s", res.Value)
	}
	entries, err := ReadSession(e.Log.Path())
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != "skill.load" || entries[0].Ref != "reg/skill/pdf" || entries[0].Revision != "v1" || entries[0].Digest != "sha256:abc" {
		t.Fatalf("entries = %+v", entries)
	}
	st, err := os.Stat(e.Log.Path())
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("log perms = %v", st.Mode().Perm())
	}
	if filepath.Dir(e.Log.Path()) != filepath.Join(dir, "agent-marketplace", "mcp-sessions") {
		t.Fatalf("log path = %s", e.Log.Path())
	}
}

// spec: code-mode-mcp/stale-snapshot-warns
func TestSearchStaleWarning(t *testing.T) {
	e, _ := testEngine(t, nil)
	e.Snapshots.Store(&Snapshot{Stale: true})
	args, _ := json.Marshal(map[string]any{"code": `async () => 1`})
	_, warnings, err := e.Search(context.Background(), args)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(warnings) != 1 || warnings[0] != ErrCodeCatalogStale {
		t.Fatalf("warnings = %v", warnings)
	}
}

// spec: code-mode-mcp/colliding-aliases-disambiguated-by-registry
func TestExecuteAliasDisambiguation(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "a/mcp/srv", Kind: "mcp", Name: "srv", Registry: "a", Assessment: "allow"},
		{Ref: "b/mcp/srv", Kind: "mcp", Name: "srv", Registry: "b", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"a/mcp/srv", "b/mcp/srv"},
		"code":    `async () => [typeof mcp["a-srv"], typeof mcp["b-srv"]]`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(res.Value), "object") {
		t.Fatalf("aliases missing: %s", res.Value)
	}
}

func TestExecuteTypescriptStripped(t *testing.T) {
	e, _ := testEngine(t, nil)
	args, _ := json.Marshal(map[string]any{
		"code": `async (): Promise<number> => { const x: number = 7; return x }`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute TS: %v", err)
	}
	if string(res.Value) != "7" {
		t.Fatalf("value = %s", res.Value)
	}
}

// spec: code-mode-mcp/repeated-declared-ref-binds-once
func TestExecuteRepeatedDeclaredRefBindsOnce(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	pool := newFakePool()
	e.Pool = pool
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv", "reg/mcp/srv"},
		"code":    `async () => { const keys = Object.keys(mcp); await mcp.srv.listTools(); return keys }`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `["srv"]` {
		t.Fatalf("aliases = %s, want the resource name bound once", res.Value)
	}
	if len(pool.started) != 1 {
		t.Fatalf("server started %d times for one ref", len(pool.started))
	}
}

// spec: code-mode-mcp/failures-carry-stable-codes
func TestThrownErrorIsCodeModeError(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code": `async () => {
			try { await mcp.srv.fail() } catch (e) {
				return [e instanceof Error, e.name, e.code, e.message, e.Code === undefined, e.Message === undefined]
			}
			return "missed"
		}`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := string(res.Value)
	var decoded []any
	if err := json.Unmarshal(res.Value, &decoded); err != nil || len(decoded) != 6 {
		t.Fatalf("caught shape = %s", got)
	}
	if decoded[0] != true || decoded[1] != "CodeModeError" || decoded[4] != true || decoded[5] != true {
		t.Fatalf("caught shape = %s, want Error named CodeModeError without Go-cased fields", got)
	}
	if decoded[2] != ErrCodeToolError || decoded[3] != "boom" {
		t.Fatalf("coded fields = %s, want code %q and message %q", got, ErrCodeToolError, "boom")
	}
}

// spec: code-mode-mcp/failures-carry-stable-codes
func TestPlainCodedObjectThrowsScriptError(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => { throw {code: "resource.revoked", message: "looks coded"} }`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeScriptError {
		t.Fatalf("expected %s for a plain thrown object, got %v", ErrCodeScriptError, err)
	}
	for _, marker := range []string{"/internal/", "/pkg/mod/", "codemode/", "mcpclient/"} {
		if strings.Contains(ce.Message, marker) {
			t.Fatalf("host path leaked in script error: %q", ce.Message)
		}
	}
}

// spec: code-mode-mcp/failures-carry-stable-codes
func TestRethrownCodeModeErrorKeepsCode(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code": `async () => {
			try { await mcp.srv.fail() } catch (e) { throw e }
		}`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeToolError {
		t.Fatalf("expected rethrown %s, got %v", ErrCodeToolError, err)
	}
}

// spec: code-mode-mcp/failures-carry-stable-codes
func TestCodeModeErrorSurvivesErrorGlobalOverwrite(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code": `async () => {
			const OriginalError = Error
			Error = function () { return {} }
			try { await skills.load("reg/skill/missing") } catch (e) {
				return [e instanceof OriginalError, e.name, e.code]
			}
			return "missed"
		}`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := string(res.Value)
	var decoded []any
	if err := json.Unmarshal(res.Value, &decoded); err != nil || len(decoded) != 3 {
		t.Fatalf("caught shape = %s", got)
	}
	if decoded[0] != true || decoded[1] != "CodeModeError" || decoded[2] != ErrCodeNotFound {
		t.Fatalf("caught shape = %s, want Error named CodeModeError with code %q", got, ErrCodeNotFound)
	}
}

// spec: code-mode-mcp/host-call-cap-enforced
func TestHostCallCapAfterErrorGlobalOverwrite(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	e.Limits = Limits{HostCallLimit: 3}
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code": `async () => {
			Error = function () { return {} }
			for (let i = 0; i < 10; i++) { await mcp.srv.ping() }
		}`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeHostCallLimit {
		t.Fatalf("expected %s, got %v", ErrCodeHostCallLimit, err)
	}
}

type findingsSkills struct {
	fakeSkills
	findings []Finding
}

func (f findingsSkills) Load(ctx context.Context, res *Resource) (SkillContent, error) {
	c, err := f.fakeSkills.Load(ctx, res)
	c.Findings = f.findings
	return c, err
}

// spec: code-mode-mcp/catalog-and-skill-findings-use-documented-fields
func TestCatalogAndSkillFindingsUseDocumentedFields(t *testing.T) {
	findings := []Finding{{Rule: "sec.high", Severity: "high", Message: "issue one"}}
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/skill/s", Kind: "skill", Name: "s", Registry: "reg", Assessment: "warn", Findings: findings},
	})
	e.Skills = findingsSkills{findings: findings}
	args, _ := json.Marshal(map[string]any{
		"code": `async () => {
			const c = catalog[0].findings[0];
			const l = (await skills.load("reg/skill/s")).findings[0];
			return [
				[c.message, c.severity, c.rule, c.Message === undefined, c.Rule === undefined],
				[l.message, l.severity, l.rule, l.Message === undefined, l.Rule === undefined],
			]
		}`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := `[["issue one","high","sec.high",true,true],["issue one","high","sec.high",true,true]]`
	if string(res.Value) != want {
		t.Fatalf("findings = %s, want %s", res.Value, want)
	}
}

// spec: code-mode-mcp/ref-prefix-matches-registry-field
func TestCatalogRefPrefixMatchesRegistry(t *testing.T) {
	mp := &Resource{Ref: "marketplace:tools/skill/x", Kind: "skill", Name: "x", Registry: "marketplace:tools", Assessment: "allow"}
	mp.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{Kind: "skill", Name: "x"}})
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
		{Ref: "local:src/skill/y", Kind: "skill", Name: "y", Registry: "local:src", Assessment: "allow"},
		*mp,
	})
	args, _ := json.Marshal(map[string]any{
		"code": `async () => catalog.every(r => r.ref.startsWith(r.registry + "/"))`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `true` {
		t.Fatalf("ref prefix diverges from registry: %s", res.Value)
	}
}

// spec: code-mode-mcp/tool-descriptions-declare-script-visible-shapes
func TestToolDescriptionsDeclareScriptVisibleShapes(t *testing.T) {
	tools := ToolDefinitions()
	seen := map[string]string{}
	for _, td := range tools {
		name, _ := td["name"].(string)
		desc, _ := td["description"].(string)
		seen[name] = desc
	}
	for _, name := range []string{"search", "execute"} {
		desc := seen[name]
		if desc == "" {
			t.Fatalf("tool %q missing", name)
		}
		for _, want := range []string{"findings", "message", "severity", "rule", "truncated", "bytes", "prefix", "CodeModeError", "code"} {
			if !strings.Contains(desc, want) {
				t.Fatalf("%s description missing %q:\n%s", name, want, desc)
			}
		}
		if !strings.Contains(desc, "interface CodeModeError extends Error") {
			t.Fatalf("%s description must declare CodeModeError as a type-only interface:\n%s", name, desc)
		}
		if strings.Contains(desc, "declare class CodeModeError") {
			t.Fatalf("%s description must not declare an ambient CodeModeError class:\n%s", name, desc)
		}
	}
}
