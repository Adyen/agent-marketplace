package ownership

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSecret and friends are recognizable non-credential stand-ins. Every leak
// assertion in this package searches for these exact strings in state, journals,
// and backup metadata.
const (
	fakeSecret  = "NOT-A-REAL-SECRET-c0ffee"
	fakeCommand = "/usr/local/bin/leak-me --token=NOT-A-REAL-SECRET-c0ffee"
	fakePrompt  = "You are a bot. The password is NOT-A-REAL-SECRET-c0ffee."
	fakeMatcher = "Bash(rm -rf /NOT-A-REAL-SECRET-c0ffee)"
	fakeHeader  = "Bearer NOT-A-REAL-SECRET-c0ffee"
)

// fixture is one isolated target root plus its isolated XDG state root.
type fixture struct {
	t          *testing.T
	targetRoot string
	stateRoot  string
	rel        string
	absFile    string
	clock      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	targetRoot := filepath.Join(base, "repo", ".devin")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		t:          t,
		targetRoot: targetRoot,
		stateRoot:  filepath.Join(base, "state", "agent-marketplace"),
		rel:        "mcp_config.json",
		clock:      time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	}
	f.absFile = filepath.Join(targetRoot, f.rel)
	return f
}

// store opens a store against the fixture. Extra options are appended last so a
// test can install a crasher.
func (f *fixture) store(opts ...Option) *Store {
	f.t.Helper()
	base := []Option{WithStateRoot(f.stateRoot), WithClock(f.nextClock)}
	st, err := Open(f.targetRoot, append(base, opts...)...)
	if err != nil {
		f.t.Fatalf("Open: %v", err)
	}
	return st
}

// nextClock advances a deterministic clock so successive backups get distinct,
// lexically ordered names without depending on wall-clock resolution.
func (f *fixture) nextClock() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *fixture) writeAggregate(content string) {
	f.t.Helper()
	if err := os.WriteFile(f.absFile, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) readAggregate() (string, bool) {
	f.t.Helper()
	data, err := os.ReadFile(f.absFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		f.t.Fatal(err)
	}
	return string(data), true
}

// mcpEntry builds a valid non-secret MCP ownership entry for a server key.
func mcpEntry(key, resource, value string) Entry {
	return Entry{
		Kind:           KindMcpServer,
		Container:      "mcpServers",
		Key:            key,
		IdentityDigest: McpIdentityDigest(key),
		ValueDigest:    Digest([]byte(value)),
		Resource:       resource,
	}
}

// hookEntry builds a valid hook ownership entry. The identity is derived from
// the event name and the digest of the canonical JSON value, so the hook payload
// itself never reaches state.
func hookEntry(event, resource, value string) Entry {
	return Entry{
		Kind:           KindHook,
		Container:      event,
		IdentityDigest: HookIdentityDigest(event, Digest([]byte(value))),
		ValueDigest:    Digest([]byte(value)),
		Resource:       resource,
	}
}

func mustApply(t *testing.T, st *Store, m Mutation) Result {
	t.Helper()
	if m.ExpectPriorGeneration == 0 && m.ExpectPriorDigest == "" {
		proof, _ := st.Proof(m.Rel)
		m.ExpectPriorGeneration = proof.Generation()
		if raw, err := os.ReadFile(m.AbsFile); err == nil {
			m.ExpectPriorDigest = Digest(raw)
		} else if !os.IsNotExist(err) {
			t.Fatalf("read aggregate for prior digest: %v", err)
		}
	}
	res, err := st.Apply(m)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatal(err)
	return false
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}
