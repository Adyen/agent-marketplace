package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type memGitHub struct {
	blobs     map[string]string
	trees     map[string][]map[string]any
	commits   map[string]map[string]any
	refs      map[string]string
	shaN      int
	requests  []string
	failPatch bool
	truncate  bool
	failures  map[string]int
}

func newMemGitHub() *memGitHub {
	return &memGitHub{
		blobs:    map[string]string{},
		trees:    map[string][]map[string]any{},
		commits:  map[string]map[string]any{},
		refs:     map[string]string{},
		failures: map[string]int{},
	}
}

func (m *memGitHub) sha() string {
	m.shaN++
	return fmt.Sprintf("sha%04d", m.shaN)
}

func (m *memGitHub) addBlob(content string) string {
	s := m.sha()
	m.blobs[s] = content
	return s
}

func (m *memGitHub) addTree(entries []map[string]any) string {
	s := m.sha()
	m.trees[s] = entries
	return s
}

func (m *memGitHub) addCommit(tree, parent string) string {
	s := m.sha()
	parents := []any{}
	if parent != "" {
		parents = []any{parent}
	}
	m.commits[s] = map[string]any{"sha": s, "tree": map[string]any{"sha": tree}, "parents": parents}
	return s
}

func (m *memGitHub) populate(version, formula string) string {
	entries := []map[string]any{
		{"path": "VERSION", "mode": "100644", "type": "blob", "sha": m.addBlob(version)},
		{"path": "Formula", "mode": "040000", "type": "tree", "sha": m.sha()},
		{"path": "Formula/agent-marketplace.rb", "mode": "100644", "type": "blob", "sha": m.addBlob(formula)},
		{"path": "other", "mode": "040000", "type": "tree", "sha": m.sha()},
		{"path": "other/readme.md", "mode": "100644", "type": "blob", "sha": m.addBlob("unrelated")},
	}
	tree := m.addTree(entries)
	commit := m.addCommit(tree, "")
	m.refs["heads/homebrew"] = commit
	return commit
}

func (m *memGitHub) treeEntry(t *testing.T, treeSHA, path string) map[string]any {
	t.Helper()
	for _, e := range m.trees[treeSHA] {
		if e["path"] == path {
			return e
		}
	}
	return nil
}

func (m *memGitHub) handle(t *testing.T, method, endpoint string, body []byte) (int, string) {
	t.Helper()
	m.requests = append(m.requests, method+" "+endpoint)
	key := method + " " + endpoint
	for prefix, status := range m.failures {
		if strings.HasPrefix(key, prefix) {
			return status, `{"message":"forced failure"}`
		}
	}
	switch {
	case method == "GET" && endpoint == "repos/o/r/git/ref/heads/homebrew":
		sha, ok := m.refs["heads/homebrew"]
		if !ok {
			return 404, `{"message":"Not Found"}`
		}
		out, _ := json.Marshal(map[string]any{"ref": "refs/heads/homebrew", "object": map[string]any{"sha": sha, "type": "commit"}})
		return 200, string(out)
	case method == "GET" && strings.HasPrefix(endpoint, "repos/o/r/git/commits/"):
		c, ok := m.commits[strings.TrimPrefix(endpoint, "repos/o/r/git/commits/")]
		if !ok {
			return 404, `{"message":"Not Found"}`
		}
		out, _ := json.Marshal(c)
		return 200, string(out)
	case method == "GET" && strings.HasPrefix(endpoint, "repos/o/r/git/trees/"):
		sha := strings.TrimSuffix(strings.TrimPrefix(endpoint, "repos/o/r/git/trees/"), "?recursive=1")
		entries, ok := m.trees[sha]
		if !ok {
			return 404, `{"message":"Not Found"}`
		}
		var withMeta []map[string]any
		for _, e := range entries {
			c := map[string]any{}
			for k, v := range e {
				c[k] = v
			}
			c["size"] = 127
			c["url"] = "https://api.github.com/repos/o/r/git/blobs/" + fmt.Sprint(e["sha"])
			withMeta = append(withMeta, c)
		}
		out, _ := json.Marshal(map[string]any{"sha": sha, "tree": withMeta, "truncated": m.truncate})
		return 200, string(out)
	case method == "GET" && strings.HasPrefix(endpoint, "repos/o/r/git/blobs/"):
		content, ok := m.blobs[strings.TrimPrefix(endpoint, "repos/o/r/git/blobs/")]
		if !ok {
			return 404, `{"message":"Not Found"}`
		}
		out, _ := json.Marshal(map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
		return 200, string(out)
	case method == "POST" && endpoint == "repos/o/r/git/trees":
		var in struct {
			BaseTree string           `json:"base_tree"`
			Tree     []map[string]any `json:"tree"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			return 422, `{"message":"bad tree"}`
		}
		var entries []map[string]any
		if in.BaseTree != "" {
			base, ok := m.trees[in.BaseTree]
			if !ok {
				return 422, `{"message":"bad base_tree"}`
			}
			entries = append(entries, base...)
		}
		for _, e := range in.Tree {
			content, _ := e["content"].(string)
			e = map[string]any{"path": e["path"], "mode": e["mode"], "type": "blob", "sha": m.addBlob(content)}
			replaced := false
			for i, old := range entries {
				if old["path"] == e["path"] {
					entries[i] = e
					replaced = true
				}
			}
			if !replaced {
				entries = append(entries, e)
			}
		}
		return 201, fmt.Sprintf(`{"sha":%q}`, m.addTree(entries))
	case method == "POST" && endpoint == "repos/o/r/git/commits":
		var in struct {
			Tree    string   `json:"tree"`
			Parents []string `json:"parents"`
			Message string   `json:"message"`
		}
		if err := json.Unmarshal(body, &in); err != nil || in.Tree == "" {
			return 422, `{"message":"bad commit"}`
		}
		sha := m.sha()
		parents := []any{}
		for _, p := range in.Parents {
			parents = append(parents, p)
		}
		m.commits[sha] = map[string]any{"sha": sha, "tree": map[string]any{"sha": in.Tree}, "parents": parents}
		return 201, fmt.Sprintf(`{"sha":%q}`, sha)
	case method == "POST" && endpoint == "repos/o/r/git/refs":
		var in struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(body, &in); err != nil || in.Ref != "refs/heads/homebrew" {
			return 422, `{"message":"bad ref"}`
		}
		if _, exists := m.refs["heads/homebrew"]; exists {
			return 422, `{"message":"Reference already exists"}`
		}
		if _, ok := m.commits[in.SHA]; !ok {
			return 422, `{"message":"bad sha"}`
		}
		m.refs["heads/homebrew"] = in.SHA
		return 201, `{"ref":"refs/heads/homebrew"}`
	case method == "PATCH" && endpoint == "repos/o/r/git/refs/heads/homebrew":
		var in struct {
			SHA   string `json:"sha"`
			Force bool   `json:"force"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			return 422, `{"message":"bad ref update"}`
		}
		if in.Force {
			t.Error("ref update must not force")
			return 422, `{"message":"forced update refused"}`
		}
		if m.failPatch {
			return 422, `{"message":"Update is not a fast forward"}`
		}
		if _, ok := m.commits[in.SHA]; !ok {
			return 422, `{"message":"bad sha"}`
		}
		m.refs["heads/homebrew"] = in.SHA
		return 200, `{"ref":"refs/heads/homebrew"}`
	}
	t.Fatalf("unexpected gh request: %s %s", method, endpoint)
	return 0, ""
}

func useMemGitHub(t *testing.T, m *memGitHub) {
	t.Helper()
	orig := ghExec
	ghExec = func(args []string, stdin []byte) ([]byte, string, int) {
		method, endpoint := "GET", ""
		for i := 0; i < len(args); i++ {
			if args[i] == "--method" && i+1 < len(args) {
				method = args[i+1]
			}
			if args[i] == "api" || (strings.HasPrefix(args[i], "repos/")) {
				if strings.HasPrefix(args[i], "repos/") {
					endpoint = args[i]
				}
			}
		}
		if endpoint == "" {
			t.Fatalf("ghExec args missing endpoint: %v", args)
		}
		status, out := m.handle(t, method, endpoint, stdin)
		if status >= 400 {
			return nil, fmt.Sprintf("gh: error (HTTP %d)", status), 1
		}
		return []byte(out), "", 0
	}
	t.Cleanup(func() { ghExec = orig })
}

func writeInput(t *testing.T, version, formula string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Formula"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Formula", "agent-marketplace.rb"), []byte(formula), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mutationRequests(m *memGitHub) []string {
	var out []string
	for _, r := range m.requests {
		if strings.HasPrefix(r, "POST") || strings.HasPrefix(r, "PATCH") {
			out = append(out, r)
		}
	}
	return out
}

// spec: homebrew-packaging/tap-branch-creation-and-update-preserve-unrelated-files
func TestPublishCreatesAbsentBranch(t *testing.T) {
	m := newMemGitHub()
	useMemGitHub(t, m)
	dir := writeInput(t, "1.2.3\n", "formula-bytes")
	if err := publish("o/r", dir); err != nil {
		t.Fatalf("publish: %v", err)
	}
	head := m.refs["heads/homebrew"]
	if head == "" {
		t.Fatal("homebrew ref not created")
	}
	treeSHA := m.commits[head]["tree"].(map[string]any)["sha"].(string)
	if got := len(m.trees[treeSHA]); got != 2 {
		t.Errorf("initial tree entries = %d, want exactly the two owned paths", got)
	}
	if parents := m.commits[head]["parents"].([]any); len(parents) != 0 {
		t.Errorf("initial commit parents = %v, want none", parents)
	}
	if m.treeEntry(t, treeSHA, "VERSION") == nil || m.treeEntry(t, treeSHA, "Formula/agent-marketplace.rb") == nil {
		t.Error("owned paths missing from initial tree")
	}
}

// spec: homebrew-packaging/tap-branch-creation-and-update-preserve-unrelated-files
func TestPublishUpdatePreservesUnrelated(t *testing.T) {
	m := newMemGitHub()
	oldHead := m.populate("1.2.3\n", "old-formula")
	useMemGitHub(t, m)
	dir := writeInput(t, "1.3.0\n", "new-formula")
	if err := publish("o/r", dir); err != nil {
		t.Fatalf("publish: %v", err)
	}
	head := m.refs["heads/homebrew"]
	if head == oldHead {
		t.Fatal("ref did not advance")
	}
	commit := m.commits[head]
	if parents := commit["parents"].([]any); len(parents) != 1 || parents[0] != oldHead {
		t.Errorf("new commit parents = %v, want [%s]", parents, oldHead)
	}
	treeSHA := commit["tree"].(map[string]any)["sha"].(string)
	unrelated := m.treeEntry(t, treeSHA, "other/readme.md")
	if unrelated == nil || m.blobs[unrelated["sha"].(string)] != "unrelated" {
		t.Error("unrelated file not preserved")
	}
	if v := m.treeEntry(t, treeSHA, "VERSION"); m.blobs[v["sha"].(string)] != "1.3.0\n" {
		t.Errorf("VERSION = %q", m.blobs[v["sha"].(string)])
	}
	if f := m.treeEntry(t, treeSHA, "Formula/agent-marketplace.rb"); m.blobs[f["sha"].(string)] != "new-formula" {
		t.Errorf("formula = %q", m.blobs[f["sha"].(string)])
	}
	if got := len(m.trees[treeSHA]); got != 5 {
		t.Errorf("tree entries = %d, want 5 (two owned + two directories + one unrelated)", got)
	}
}

// spec: homebrew-packaging/older-release-cannot-replace-a-newer-formula
func TestPublishSkipsOlderVersion(t *testing.T) {
	m := newMemGitHub()
	head := m.populate("1.10.0\n", "newer-formula")
	useMemGitHub(t, m)
	dir := writeInput(t, "1.9.0\n", "older-formula")
	if err := publish("o/r", dir); err != nil {
		t.Fatalf("older release must be skipped, not fail: %v", err)
	}
	if n := len(mutationRequests(m)); n != 0 {
		t.Errorf("older release mutated the branch: %v", mutationRequests(m))
	}
	if m.refs["heads/homebrew"] != head {
		t.Error("ref moved on older release")
	}
}

// spec: homebrew-packaging/identical-release-is-idempotent
func TestPublishIdenticalIsNoOp(t *testing.T) {
	m := newMemGitHub()
	head := m.populate("1.2.3\n", "same-formula")
	useMemGitHub(t, m)
	dir := writeInput(t, "1.2.3\n", "same-formula")
	if err := publish("o/r", dir); err != nil {
		t.Fatalf("identical publish must be a no-op: %v", err)
	}
	if n := len(mutationRequests(m)); n != 0 {
		t.Errorf("idempotent publish mutated the branch: %v", mutationRequests(m))
	}
	if m.refs["heads/homebrew"] != head {
		t.Error("ref moved on identical publish")
	}
}

// spec: homebrew-packaging/same-version-output-conflict-fails-closed
func TestPublishSameVersionConflictFails(t *testing.T) {
	m := newMemGitHub()
	head := m.populate("1.2.3\n", "published-formula")
	useMemGitHub(t, m)
	dir := writeInput(t, "1.2.3\n", "different-formula")
	if err := publish("o/r", dir); err == nil {
		t.Fatal("same-version differing output must fail")
	}
	if n := len(mutationRequests(m)); n != 0 {
		t.Errorf("conflict mutated the branch: %v", mutationRequests(m))
	}
	if m.refs["heads/homebrew"] != head {
		t.Error("ref moved on conflict")
	}
}

// spec: homebrew-packaging/malformed-tap-version-fails-closed
func TestPublishMalformedStateFails(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(m *memGitHub)
		input   func(t *testing.T) string
	}{
		{"malformed VERSION", func(m *memGitHub) { m.populate("vbad\n", "f") }, nil},
		{"missing VERSION entry", func(m *memGitHub) {
			tree := m.addTree([]map[string]any{
				{"path": "Formula/agent-marketplace.rb", "mode": "100644", "type": "blob", "sha": m.addBlob("f")},
			})
			m.refs["heads/homebrew"] = m.addCommit(tree, "")
		}, nil},
		{"missing formula entry", func(m *memGitHub) {
			tree := m.addTree([]map[string]any{
				{"path": "VERSION", "mode": "100644", "type": "blob", "sha": m.addBlob("1.2.3\n")},
			})
			m.refs["heads/homebrew"] = m.addCommit(tree, "")
		}, nil},
		{"symlink VERSION entry", func(m *memGitHub) {
			tree := m.addTree([]map[string]any{
				{"path": "VERSION", "mode": "120000", "type": "blob", "sha": m.addBlob("1.0.0")},
				{"path": "Formula/agent-marketplace.rb", "mode": "100644", "type": "blob", "sha": m.addBlob("f")},
			})
			m.refs["heads/homebrew"] = m.addCommit(tree, "")
		}, nil},
		{"truncated tree", func(m *memGitHub) { m.populate("1.2.3\n", "f"); m.truncate = true }, nil},
		{"malformed input", func(m *memGitHub) { m.populate("1.2.3\n", "f") }, func(t *testing.T) string {
			t.Helper()
			return writeInput(t, "not-a-version\n", "f")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMemGitHub()
			tc.prepare(m)
			head := m.refs["heads/homebrew"]
			useMemGitHub(t, m)
			dir := writeInput(t, "1.3.0\n", "new")
			if tc.input != nil {
				dir = tc.input(t)
			}
			if err := publish("o/r", dir); err == nil {
				t.Fatal("malformed state must fail")
			}
			if n := len(mutationRequests(m)); n != 0 {
				t.Errorf("malformed state mutated the branch: %v", mutationRequests(m))
			}
			if m.refs["heads/homebrew"] != head {
				t.Error("ref moved on malformed state")
			}
		})
	}
}

// spec: homebrew-packaging/stale-publication-conflict-leaves-the-branch-unchanged
func TestPublishStaleRefConflictFails(t *testing.T) {
	m := newMemGitHub()
	head := m.populate("1.2.3\n", "old")
	m.failPatch = true
	useMemGitHub(t, m)
	dir := writeInput(t, "1.3.0\n", "new")
	if err := publish("o/r", dir); err == nil {
		t.Fatal("non-fast-forward ref update must fail visibly")
	}
	if m.refs["heads/homebrew"] != head {
		t.Error("ref moved despite rejected update")
	}
}

// spec: homebrew-packaging/stale-publication-conflict-leaves-the-branch-unchanged
func TestPublishAPIFailuresSurface(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		status int
	}{
		{"ref read forbidden", "GET repos/o/r/git/ref", 403},
		{"ref read server error", "GET repos/o/r/git/ref", 500},
		{"tree create failure", "POST repos/o/r/git/trees", 500},
		{"commit create failure", "POST repos/o/r/git/commits", 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMemGitHub()
			head := m.populate("1.2.3\n", "old")
			m.failures[tc.prefix] = tc.status
			useMemGitHub(t, m)
			dir := writeInput(t, "1.3.0\n", "new")
			if err := publish("o/r", dir); err == nil {
				t.Fatal("API failure must surface")
			}
			if m.refs["heads/homebrew"] != head {
				t.Error("ref moved despite API failure")
			}
		})
	}
}

// spec: homebrew-packaging/stale-publication-conflict-leaves-the-branch-unchanged
func TestGHAPIReportsMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out, status, err := ghAPI("GET", "repos/o/r", nil)
	if err == nil {
		t.Fatalf("ghAPI succeeded with no gh executable: %s", out)
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 (process start failure is not an HTTP status)", status)
	}
	if !strings.Contains(err.Error(), exec.ErrNotFound.Error()) {
		t.Errorf("error %q must report the underlying start failure %q", err, exec.ErrNotFound.Error())
	}
}

// spec: homebrew-packaging/stale-publication-conflict-leaves-the-branch-unchanged
func TestPublishSubprocessFakeGH(t *testing.T) {
	dir := t.TempDir()
	gh := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$GH_LOG\"\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    *ref/heads/homebrew)\n" +
		"      echo 'gh: Not Found (HTTP 404)' >&2; exit 1;;\n" +
		"  esac\n" +
		"done\n" +
		"case \"$*\" in\n" +
		"  *git/trees*) echo '{\"sha\":\"treeX\"}';;\n" +
		"  *git/commits*) echo '{\"sha\":\"commitX\"}';;\n" +
		"  *git/refs*) echo '{\"ref\":\"refs/heads/homebrew\"}';;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	t.Setenv("GH_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	input := writeInput(t, "1.2.3\n", "formula")
	if err := run([]string{"publish", "--repository", "o/r", "--input", input}); err != nil {
		t.Fatalf("publish via fake gh: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"git/ref/heads/homebrew", "git/trees", "git/commits", "git/refs"} {
		if !strings.Contains(got, want) {
			t.Errorf("fake gh log missing %s:\n%s", want, got)
		}
	}
}
