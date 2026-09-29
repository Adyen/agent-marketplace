package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSnapshotFindConcurrent(t *testing.T) {
	snap := &Snapshot{Resources: []Resource{
		{Ref: "reg/skill/a", Kind: "skill", Name: "a"},
		{Ref: "reg/mcp/b", Kind: "mcp", Name: "b"},
	}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if snap.Find("reg/skill/a") == nil || snap.Find("reg/mcp/b") == nil {
					t.Error("Find returned nil")
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestExecuteHostCallHonorsContext(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	e.Pool = &blockingPool{}
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => mcp.srv.ping()`,
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	done := make(chan error, 1)
	go func() {
		_, _, err := e.Execute(ctx, args)
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("blocked host call did not honor the call context")
	}
}

type blockingPool struct{ fakePool }

func (p *blockingPool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("host call returned without context cancellation")
	}
}

// spec: code-mode-mcp/deadline-bounds-execution
func TestExecuteDeadlineBoundsBlockedHostCall(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	e.Limits = Limits{Deadline: 75 * time.Millisecond}
	pool := &deadlinePool{}
	e.Pool = pool
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => mcp.srv.ping()`,
	})
	done := make(chan error, 1)
	go func() {
		_, _, err := e.Execute(context.Background(), args)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked host call returned success at the deadline")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked host call was not bounded by the VM deadline")
	}
	if !pool.bounded {
		t.Fatal("host call context carried no deadline")
	}
}

type deadlinePool struct {
	fakePool
	bounded bool
}

func (p *deadlinePool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	_, p.bounded = ctx.Deadline()
	<-ctx.Done()
	return nil, ctx.Err()
}

// spec: code-mode-mcp/declared-servers-gated-before-code-runs
func TestExecuteGatesDeclaredServersBeforeCode(t *testing.T) {
	res := []Resource{
		{Ref: "reg/mcp/revoked", Kind: "mcp", Name: "revoked", Registry: "reg", Assessment: "allow"},
		{Ref: "reg/mcp/ok", Kind: "mcp", Name: "ok", Registry: "reg", Assessment: "allow"},
	}
	e, _ := testEngine(t, res)
	gated := map[string]bool{}
	e.Gate = func(ctx context.Context, r *Resource) error {
		gated[r.Ref] = true
		if r.Name == "revoked" {
			return CodeError(ErrCodeRevoked, "%s is revoked", r.Ref)
		}
		return nil
	}
	pool := newFakePool()
	e.Pool = pool
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/ok", "reg/mcp/revoked"},
		"code":    `async () => mcp.ok.ping()`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeRevoked {
		t.Fatalf("expected %s, got %v", ErrCodeRevoked, err)
	}
	if !gated["reg/mcp/revoked"] || !gated["reg/mcp/ok"] {
		t.Fatalf("gate not consulted for every declared ref: %v", gated)
	}
	if len(pool.started) != 0 {
		t.Fatalf("code ran or server touched despite revoked gate: %v", pool.started)
	}
}

func TestExecuteFailsClosedWithoutGateEvidence(t *testing.T) {
	res := &Resource{Ref: "pub:x/mcp/srv", Kind: "mcp", Name: "srv", Registry: "x", Assessment: "allow"}
	res.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
		Entry:      config.MarketplaceEntry{URL: "https://marketplace.example"},
		RegistryID: "x", Kind: "mcp", Name: "srv", Revision: "r1",
	}})
	e, _ := testEngine(t, []Resource{*res})
	loader := &FileSkillLoader{Cache: marketplace.NewCache(t.TempDir()), Online: false}
	e.Gate = loader.Gate
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"pub:x/mcp/srv"},
		"code":    `async () => 1`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeNotFound {
		t.Fatalf("expected %s for missing gate evidence, got %v", ErrCodeNotFound, err)
	}
}

// spec: code-mode-mcp/traversal-and-symlink-escape-rejected
func TestSkillReadRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("external bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	res := &Resource{Ref: "reg/skill/s", Kind: "skill", Name: "s"}
	res.SetOrigin(&Origin{ContentDir: root, File: "SKILL.md"})
	loader := &FileSkillLoader{}
	for _, p := range []string{"../outside.txt", "../../etc/passwd", "leak.txt", "sub/../../outside.txt"} {
		got, err := loader.ReadFile(context.Background(), res, p)
		if err == nil {
			t.Fatalf("ReadFile(%q) = %q, expected rejection", p, got)
		}
		if strings.Contains(got, "external bytes") {
			t.Fatalf("external content read via %q", p)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loader.ReadFile(context.Background(), res, "notes.txt")
	if err != nil || got != "inside" {
		t.Fatalf("ReadFile legit = %q, %v", got, err)
	}
}

func TestSkillReadRefusesSymlinkComponents(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("external bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "inner.txt"), []byte("inner"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"alias.txt":  "real.txt",
		"diralias":   "sub",
		"escape.txt": outside,
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	res := &Resource{Ref: "reg/skill/s", Kind: "skill", Name: "s"}
	res.SetOrigin(&Origin{ContentDir: root, File: "SKILL.md"})
	loader := &FileSkillLoader{}
	for _, p := range []string{"alias.txt", "diralias/inner.txt", "escape.txt"} {
		got, err := loader.ReadFile(context.Background(), res, p)
		if err == nil {
			t.Fatalf("ReadFile(%q) through symlink = %q, expected refusal", p, got)
		}
	}
	for _, p := range []string{"real.txt", "sub/inner.txt", "SKILL.md"} {
		if _, err := loader.ReadFile(context.Background(), res, p); err != nil {
			t.Fatalf("ReadFile(%q) in-root control: %v", p, err)
		}
	}
}

func TestMarketplaceReadRejectsTraversal(t *testing.T) {
	res := &Resource{Ref: "m:x/skill/s", Kind: "skill", Name: "s"}
	res.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{Kind: "skill", Name: "s"}})
	loader := &FileSkillLoader{Cache: nil, Online: false}

	for _, p := range []string{"../escape", "a/../../b"} {
		_, err := loader.ReadFile(context.Background(), res, p)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != ErrCodeNotFound {
			t.Fatalf("ReadFile(%q): expected %s, got %v", p, ErrCodeNotFound, err)
		}
	}
}

// spec: code-mode-mcp/output-cap-truncates-to-valid-json
func TestOutputTruncatedProducesValidJSON(t *testing.T) {
	res, err := Run(context.Background(), `async () => "x".repeat(2 * 1024 * 1024)`, nil, Limits{OutputCap: 1 << 20})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("expected Truncated=true")
	}
	var decoded any
	if err := json.Unmarshal(res.Value, &decoded); err != nil {
		t.Fatalf("truncated value is not valid JSON: %v (%.80s)", err, res.Value)
	}
	m, ok := decoded.(map[string]any)
	if !ok || m["truncated"] != true {
		t.Fatalf("truncated marker missing: %.200s", res.Value)
	}
}

// spec: code-mode-mcp/loads-and-starts-recorded-with-identity
func TestServerStartLogged(t *testing.T) {
	dir := t.TempDir()
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Revision: "v9", Assessment: "allow"},
	})
	e.Log = NewSessionLog(dir, "sess-start")
	pool := &observePool{fakePool: fakePool{}}
	e.Pool = pool
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => mcp.srv.ping()`,
	})
	if _, _, err := e.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	entries, err := ReadSession(e.Log.Path())
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	var starts int
	for _, en := range entries {
		if en.Event == "server.start" {
			starts++
			if en.Ref != "reg/mcp/srv" || en.Revision != "v9" || en.Digest != "sha256:def" {
				t.Fatalf("server.start entry = %+v", en)
			}
		}
	}
	if starts != 1 {
		t.Fatalf("expected exactly one server.start entry, got %d (%+v)", starts, entries)
	}
}

type observePool struct {
	fakePool
	cb      func(string, *Resource, *ServerDef)
	def     *ServerDef
	started bool
}

func (p *observePool) ObserveStarts(fn func(string, *Resource, *ServerDef)) { p.cb = fn }

func (p *observePool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	p.fire(res)
	return p.fakePool.Call(ctx, alias, res, tool, args)
}

func (p *observePool) fire(res *Resource) {
	if p.started {
		return
	}
	p.started = true
	if p.cb != nil {
		def := p.def
		if def == nil {
			def = &ServerDef{Digest: "sha256:def"}
		}
		p.cb("", res, def)
	}
}

// spec: code-mode-mcp/revoked-or-rejected-resources-refuse
func TestSkillLoadErrorsPreserveCodedErrors(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/skill/ok", Kind: "skill", Name: "ok", Revision: "v1", Assessment: "allow"},
	})

	uncaught, _ := json.Marshal(map[string]any{
		"code": `async () => skills.load("reg/skill/missing")`,
	})
	_, _, err := e.Execute(context.Background(), uncaught)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeNotFound {
		t.Fatalf("expected %s at the tools/call boundary, got %v", ErrCodeNotFound, err)
	}

	caught, _ := json.Marshal(map[string]any{
		"code": `async () => {
			try { await skills.load("reg/skill/missing") } catch (err) { return err.Code }
			return "missed"
		}`,
	})
	res, _, err := e.Execute(context.Background(), caught)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `"`+ErrCodeNotFound+`"` {
		t.Fatalf("caught value = %s, want coded object with Code %q", res.Value, ErrCodeNotFound)
	}

	e.Gate = func(ctx context.Context, r *Resource) error {
		return CodeError(ErrCodeRevoked, "%s is revoked", r.Ref)
	}
	revoked, _ := json.Marshal(map[string]any{
		"code": `async () => skills.load("reg/skill/ok")`,
	})
	_, _, err = e.Execute(context.Background(), revoked)
	if !errors.As(err, &ce) || ce.Code != ErrCodeRevoked {
		t.Fatalf("expected %s for a gated skill, got %v", ErrCodeRevoked, err)
	}

	e.Gate = nil
	control, _ := json.Marshal(map[string]any{
		"code": `async () => (await skills.load("reg/skill/ok")).content`,
	})
	res, _, err = e.Execute(context.Background(), control)
	if err != nil {
		t.Fatalf("Execute control: %v", err)
	}
	if string(res.Value) != `"# ok"` {
		t.Fatalf("control load = %s", res.Value)
	}
}

// spec: code-mode-mcp/source-provenance-recorded
func TestSessionLogRecordsSourceProvenance(t *testing.T) {
	dir := t.TempDir()
	gitRes := &Resource{Ref: "github.com:org-repo/skill/x", Kind: "skill", Name: "x", Revision: "main", Assessment: "allow"}
	gitRes.SetOrigin(&Origin{ContentDir: t.TempDir(), SourceURL: "git@github.com:org/repo.git", SourcePath: "skills"})
	mpRes := &Resource{Ref: "mp:tools/skill/review", Kind: "skill", Name: "review", Revision: "r1", Assessment: "allow"}
	mpRes.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
		Entry:      config.MarketplaceEntry{URL: "https://mp.example"},
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "r1",
	}})
	e, _ := testEngine(t, []Resource{*gitRes, *mpRes})
	e.Log = NewSessionLog(dir, "sess-prov")
	args, _ := json.Marshal(map[string]any{
		"code": `async () => [(await skills.load("github.com:org-repo/skill/x")).content, (await skills.load("mp:tools/skill/review")).content]`,
	})
	if _, _, err := e.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	entries, err := ReadSession(e.Log.Path())
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Source != "git@github.com:org/repo.git" || entries[0].SourcePath != "skills" || entries[0].MarketplaceProvenance() {
		t.Fatalf("git provenance = %+v", entries[0])
	}
	if !entries[1].MarketplaceProvenance() || entries[1].Source != "marketplace:https://mp.example" {
		t.Fatalf("marketplace provenance = %+v", entries[1])
	}
}

func TestServerStartLogsContentDigest(t *testing.T) {
	dir := t.TempDir()
	res := &Resource{Ref: "mp:tools/mcp/remote", Kind: "mcp", Name: "remote", Revision: "r1", Assessment: "allow"}
	res.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
		Entry:      config.MarketplaceEntry{URL: "https://mp.example"},
		RegistryID: "src", Kind: "mcpserver", Name: "remote", Revision: "r1",
	}})
	e, _ := testEngine(t, []Resource{*res})
	e.Log = NewSessionLog(dir, "sess-pin")
	e.Pool = &observePool{def: &ServerDef{
		Command: "echo", Digest: "sha256:archive", ContentDigest: "sha256:content",
	}}
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"mp:tools/mcp/remote"},
		"code":    `async () => mcp.remote.ping()`,
	})
	if _, _, err := e.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	entries, err := ReadSession(e.Log.Path())
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != "server.start" {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Digest != "sha256:content" {
		t.Fatalf("server.start digest = %q, want the resource content digest for pinning", entries[0].Digest)
	}
}

// spec: code-mode-mcp/marketplace-artifacts-verified-by-digest
func TestResolveServerDefSeparatesArchiveAndContentDigests(t *testing.T) {
	loader := &FileSkillLoader{Cache: marketplace.NewCache(t.TempDir()), Online: false}
	srv := ir.NewMcpServer("remote", "echo", []string{"hi"}, nil, nil, ir.SourceRef{})
	rev, archiveDigest := seedMarketplaceArtifact(t, loader.Cache, "https://mp.example", "src", "mcpserver", "remote", "r1", srv)
	res := &Resource{Ref: "mp:tools/mcp/remote", Kind: "mcp", Name: "remote", Revision: "r1"}
	res.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
		Entry:      config.MarketplaceEntry{URL: "https://mp.example"},
		RegistryID: "src", Kind: "mcpserver", Name: "remote", Revision: "r1",
	}})
	def, err := loader.ResolveServerDef(context.Background(), res)
	if err != nil {
		t.Fatalf("ResolveServerDef: %v", err)
	}
	if def.Command != "echo" {
		t.Fatalf("def = %+v", def)
	}
	if def.Digest != archiveDigest {
		t.Fatalf("definition digest = %q, want archive digest %q", def.Digest, archiveDigest)
	}
	if def.ContentDigest != string(rev.ContentDigest) {
		t.Fatalf("content digest = %q, want %q", def.ContentDigest, rev.ContentDigest)
	}
}

func TestSnapshotRecordsCheckedOutGitCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	commitA := snapshotGitCommitPush(t, work, "a")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main"},
	}}}
	build := func() *Resource {
		snap, errs := builder.BuildSnapshot(context.Background(), ec)
		if len(errs) > 0 {
			t.Fatalf("BuildSnapshot errors: %v", errs)
		}
		if len(snap.Resources) != 1 {
			t.Fatalf("snapshot resources = %+v", snap.Resources)
		}
		return &snap.Resources[0]
	}

	res := build()
	if res.Revision != commitA {
		t.Fatalf("snapshot revision = %q, want checked-out commit %q", res.Revision, commitA)
	}

	writeSnapshotSkill(t, work, "a", "body two")
	commitB := snapshotGitCommitPush(t, work, "b")
	res = build()
	if res.Revision != commitB {
		t.Fatalf("snapshot revision after advance = %q, want %q", res.Revision, commitB)
	}
}

func TestSnapshotOmitsGitRegistryWithoutReadableCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	snapshotGitCommitPush(t, work, "a")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main"},
	}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) == 0 && !snap.Stale {
		t.Fatalf("BuildSnapshot reported no error or stale state for an unresolvable cached commit")
	}
	for _, res := range snap.Resources {
		if res.Kind == "skill" && res.Name == "a" {
			t.Fatalf("snapshot advertised unsafe git resource: %+v", res)
		}
	}
}

func TestSnapshotGitContentMatchesAdvertisedCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	commitA := snapshotGitCommitPush(t, work, "a")

	local := filepath.Join(base, "local")
	writeSnapshotSkill(t, local, "loc", "local body")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main"},
		{URL: local},
	}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	if len(snap.Resources) != 2 {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	var resA, resLocal *Resource
	for i := range snap.Resources {
		switch snap.Resources[i].Name {
		case "a":
			resA = &snap.Resources[i]
		case "loc":
			resLocal = &snap.Resources[i]
		}
	}
	if resA == nil || resLocal == nil {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	if resA.Revision != commitA {
		t.Fatalf("snapshot revision = %q, want %q", resA.Revision, commitA)
	}
	lsc, err := NewFileSkillLoader(true).Load(context.Background(), resLocal)
	if err != nil || !strings.Contains(lsc.Content, "local body") {
		t.Fatalf("local registry content = %q, err %v", lsc.Content, err)
	}

	writeSnapshotSkill(t, work, "a", "body two")
	snapshotGitCommitPush(t, work, "b")
	if _, errs := builder.BuildSnapshot(context.Background(), ec); len(errs) > 0 {
		t.Fatalf("refresh BuildSnapshot errors: %v", errs)
	}

	sc, err := NewFileSkillLoader(true).Load(context.Background(), resA)
	if err != nil {
		t.Fatalf("skills.load: %v", err)
	}
	if sc.Revision != commitA {
		t.Fatalf("loaded revision = %q, want advertised %q", sc.Revision, commitA)
	}
	if !strings.Contains(sc.Content, "body one") {
		t.Fatalf("loaded content does not match advertised commit %q: %q", commitA, sc.Content)
	}
}

func writeLocalMCPServer(t *testing.T, dir, name, command string, args ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawArgs, _ := json.Marshal(args)
	raw := fmt.Sprintf(`{"mcpServers":{%q:{"command":%q,"args":%s}}}`, name, command, rawArgs)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

type recordPool struct {
	fakePool
	calls []*Resource
}

func (p *recordPool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	p.calls = append(p.calls, res)
	return map[string]any{"tool": tool}, nil
}

func TestSnapshotDisambiguatesCollidingRegistryLabels(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	dirA := filepath.Join(base, "a", "tools")
	dirB := filepath.Join(base, "b", "tools")
	writeLocalMCPServer(t, dirA, "fake", "echo", "aaa")
	writeLocalMCPServer(t, dirB, "fake", "echo", "bbb")
	writeLocalMCPServer(t, filepath.Join(dirA, "sub"), "fake", "echo", "ccc")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: dirA},
		{URL: dirB},
		{URL: dirA, Path: "sub"},
	}}}
	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}

	build := func() *Snapshot {
		snap, errs := builder.BuildSnapshot(context.Background(), ec)
		if len(errs) > 0 {
			t.Fatalf("BuildSnapshot errors: %v", errs)
		}
		return snap
	}
	snap := build()
	if len(snap.Resources) != 3 {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}

	wantArgs := map[string]string{dirA: "aaa", dirB: "bbb", filepath.Join(dirA, "sub"): "ccc"}
	seen := map[string]string{}
	for i := range snap.Resources {
		res := &snap.Resources[i]
		if res.Kind != "mcp" {
			t.Fatalf("unexpected resource %+v", res)
		}
		if !strings.HasPrefix(res.Ref, "local:tools") {
			t.Fatalf("ref %q lost the local label prefix", res.Ref)
		}
		if prev, dup := seen[res.Ref]; dup {
			t.Fatalf("ref %q collides between %s and %s", res.Ref, prev, res.Origin().ContentDir)
		}
		seen[res.Ref] = res.Origin().ContentDir
		if got := snap.Find(res.Ref); got == nil || got.Ref != res.Ref {
			t.Fatalf("Find(%q) = %+v", res.Ref, got)
		}
		def := res.ServerDef()
		if def == nil {
			t.Fatalf("resource %+v has no server definition", res)
		}
		if want, ok := wantArgs[res.Origin().ContentDir]; !ok || len(def.Args) == 0 || def.Args[0] != want {
			t.Fatalf("ref %q routed to definition %v from %s", res.Ref, def.Args, res.Origin().ContentDir)
		}
	}
	if got := snap.Find("local:tools/mcp/fake"); got != nil {
		t.Fatalf("ambiguous legacy ref still resolves to %+v", got)
	}

	snap2 := build()
	if len(snap2.Resources) != len(snap.Resources) {
		t.Fatalf("second build resources = %+v", snap2.Resources)
	}
	for i := range snap.Resources {
		if snap.Resources[i].Ref != snap2.Resources[i].Ref {
			t.Fatalf("ref %q is not deterministic; second build has %q", snap.Resources[i].Ref, snap2.Resources[i].Ref)
		}
	}

	e, _ := testEngine(t, snap.Resources)
	pool := &recordPool{}
	e.Pool = pool
	for _, res := range snap.Resources {
		want := wantArgs[res.Origin().ContentDir]
		args, _ := json.Marshal(map[string]any{
			"servers": []string{res.Ref},
			"code":    `async () => mcp.fake.ping()`,
		})
		if _, _, err := e.Execute(context.Background(), args); err != nil {
			t.Fatalf("Execute(%s): %v", res.Ref, err)
		}
		last := pool.calls[len(pool.calls)-1]
		if last.Ref != res.Ref || last.ServerDef().Args[0] != want {
			t.Fatalf("declared ref %q routed to %+v", res.Ref, last)
		}
	}
}

func TestSnapshotLabelsSurviveAdversarialSuffixAndIdenticalSources(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	dirA := filepath.Join(base, "a", "tools")
	dirB := filepath.Join(base, "b", "tools")
	dirD := filepath.Join(base, "d", "other")
	writeLocalMCPServer(t, dirA, "fake", "echo", "aaa")
	writeLocalMCPServer(t, dirB, "fake", "echo", "bbb")
	writeLocalMCPServer(t, dirD, "fake", "echo", "ddd")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	build := func(entries ...config.RegistryEntry) *Snapshot {
		ec := config.EffectiveConfig{Merged: &config.Config{Registries: entries}}
		snap, errs := builder.BuildSnapshot(context.Background(), ec)
		if len(errs) > 0 {
			t.Fatalf("BuildSnapshot errors: %v", errs)
		}
		return snap
	}

	snap := build(config.RegistryEntry{URL: dirA}, config.RegistryEntry{URL: dirB})
	if len(snap.Resources) != 2 {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	var labelA string
	for i := range snap.Resources {
		res := &snap.Resources[i]
		if res.Origin().ContentDir == dirA {
			labelA = strings.TrimSuffix(res.Ref, "/mcp/fake")
		}
	}
	if !strings.HasPrefix(labelA, "local:tools-") {
		t.Fatalf("dirA label = %q, expected a suffixed local label", labelA)
	}
	dirC := filepath.Join(base, "c", strings.TrimPrefix(labelA, "local:"))
	writeLocalMCPServer(t, dirC, "fake", "echo", "ccc")

	entries := []config.RegistryEntry{
		{URL: dirA},
		{URL: dirB},
		{URL: dirC},
		{URL: dirD},
		{URL: dirA},
	}
	snap = build(entries...)
	if len(snap.Resources) != 4 {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	wantArgs := map[string]string{dirA: "aaa", dirB: "bbb", dirC: "ccc", dirD: "ddd"}
	seen := map[string]string{}
	for i := range snap.Resources {
		res := &snap.Resources[i]
		if prev, dup := seen[res.Ref]; dup {
			t.Fatalf("ref %q shared by %s and %s", res.Ref, prev, res.Origin().ContentDir)
		}
		seen[res.Ref] = res.Origin().ContentDir
		if !SanitizeRef(res.Ref) {
			t.Fatalf("ref %q fails SanitizeRef", res.Ref)
		}
		got := snap.Find(res.Ref)
		if got == nil || got.Origin().ContentDir != res.Origin().ContentDir {
			t.Fatalf("Find(%q) misroutes to %+v", res.Ref, got)
		}
		def := res.ServerDef()
		if def == nil || len(def.Args) == 0 || def.Args[0] != wantArgs[res.Origin().ContentDir] {
			t.Fatalf("ref %q routed to wrong definition %+v", res.Ref, def)
		}
	}
	if ref := "local:other/mcp/fake"; snap.Find(ref) == nil {
		t.Fatalf("unambiguous ref %q was modified", ref)
	}

	snap2 := build(entries...)
	if len(snap2.Resources) != len(snap.Resources) {
		t.Fatalf("second build resources = %+v", snap2.Resources)
	}
	for i := range snap.Resources {
		if snap.Resources[i].Ref != snap2.Resources[i].Ref {
			t.Fatalf("ref %q is not deterministic; second build has %q", snap.Resources[i].Ref, snap2.Resources[i].Ref)
		}
	}

	e, _ := testEngine(t, snap.Resources)
	pool := &recordPool{}
	e.Pool = pool
	for _, res := range snap.Resources {
		want := wantArgs[res.Origin().ContentDir]
		args, _ := json.Marshal(map[string]any{
			"servers": []string{res.Ref},
			"code":    `async () => mcp.fake.ping()`,
		})
		if _, _, err := e.Execute(context.Background(), args); err != nil {
			t.Fatalf("Execute(%s): %v", res.Ref, err)
		}
		last := pool.calls[len(pool.calls)-1]
		if last.Ref != res.Ref || last.ServerDef().Args[0] != want {
			t.Fatalf("declared ref %q routed to %+v", res.Ref, last)
		}
	}
}

// spec: code-mode-mcp/ambiguous-alias-fails-closed
func TestExecuteFailsClosedOnAmbiguousAlias(t *testing.T) {
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/one", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
		{Ref: "reg/mcp/two", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/one", "reg/mcp/two"},
		"code":    `async () => mcp["reg-srv"].ping()`,
	})
	_, _, err := e.Execute(context.Background(), args)
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeInvalidArguments {
		t.Fatalf("expected %s for colliding aliases, got %v", ErrCodeInvalidArguments, err)
	}
}

func snapshotGit(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func snapshotGitCommitPush(t testing.TB, work, message string) string {
	t.Helper()
	snapshotGit(t, work, "add", ".")
	snapshotGit(t, work, "commit", "-m", message)
	snapshotGit(t, work, "push", "origin", "main")
	return snapshotGit(t, work, "rev-parse", "HEAD")
}

func writeSnapshotSkill(t testing.TB, repoDir, name, description string) {
	t.Helper()
	skillDir := filepath.Join(repoDir, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n" + description + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func seedMarketplaceArtifact(t *testing.T, c *marketplace.Cache, baseURL, registryID, kind, name, revision string, resource ir.Resource) (store.AssessedResourceRevision, string) {
	t.Helper()
	artifact, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	artifact.Manifest.RegistryID = registryID
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               store.ResourceCoordinate{Marketplace: "mp", Registry: store.RegistryID(registryID), Kind: kind, Name: name},
		ContentDigest:            store.RevisionDigest(artifact.Manifest.ContentDigest),
		AnalyzerVersion:          "test/v1",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	rev := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: store.RegistryID(registryID), Kind: kind, Name: name},
			ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		Assessment: assessment,
	}
	if _, err := c.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if err := c.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("MarkArtifactVerified: %v", err)
	}
	if _, err := c.WriteResourceRevision(baseURL, registryID, kind, name, revision, rev); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	return rev, archiveDigest
}
