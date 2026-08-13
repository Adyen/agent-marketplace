package ownership

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file is B2's acceptance suite: the tests that prove Apply, Recover, and
// BackupOnly are serialized per canonical target root, and that no read path is.
//
// Two rules shape every test here.
//
// 1. NOTHING IS PROVEN BY SAMPLING. A test that races two writers and asserts
// "no corruption" also passes on a tree where the window merely got smaller,
// which is worse than no test at all because it makes the repro flaky instead of
// failing. So the primary instrument is the O_EXCL SENTINEL: each critical
// section exclusively creates one file on entry and removes it on exit, so an
// overlap of even one instruction is reported by the kernel as EEXIST. That
// converts "prove an absence of interleaving" into "prove one positive event
// never happens".
//
// The sentinel is created by a [Crasher] installed at Apply's own crash points,
// NOT by a lock observer. That is deliberate and it is what makes the instrument
// non-vacuous: the bracket lives inside Apply's body and exists whether or not
// Apply acquires anything, so deleting the acquisition makes these tests fail
// rather than making the sentinel disappear.
//
// 2. NO SLEEP-BASED SYNCHRONIZATION. In-process racers rendezvous on a channel,
// cross-process racers on pipes, and every test that needs a writer to be
// provably inside its section while another arrives constructs that interleaving
// with a real holder rather than hoping for it.

// ---------------------------------------------------------------------------
// The O_EXCL sentinel harness
// ---------------------------------------------------------------------------

// sectionSentinel brackets Apply's body with an exclusively created file.
//
// CrashBeforeBackup is the first crash point Apply reaches (immediately after
// validation, before a single byte is written) and CrashAfterJournalCleanup is
// the last (the generation is complete), so the bracket covers every WRITE the
// mutation performs: the mandatory safety backup, the journal, the staged write,
// the rename, and the state commit.
//
// The bracket is therefore a SUBSET of the critical section, not the whole of
// it: validation, the Load that picks the generation number, and nextRecord all
// run before CrashBeforeBackup (journal.go — Load precedes crash point 1). That
// costs the instrument nothing in practice, because those steps take
// microseconds and are followed unconditionally by the create: a second writer
// that overlaps the bracket at all reaches CrashBeforeBackup while the first is
// still inside it, and the kernel reports EEXIST. The only interleaving the
// bracket cannot see is one confined entirely to the pre-Load window, which
// writes nothing.
//
// It never actually crashes anything: Crash returns nil at every point except an
// overlap, which is the one event the harness exists to report.
type sectionSentinel struct {
	path string
}

func (s *sectionSentinel) Crash(point CrashPoint) error {
	switch point {
	case CrashBeforeBackup:
		f, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("two ownership critical sections overlapped on one canonical root: %w", err)
		}
		return f.Close()
	case CrashAfterJournalCleanup:
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("sentinel vanished from inside the section: %w", err)
		}
		return nil
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// A realistic read-modify-commit, shared by the in-process and child racers
// ---------------------------------------------------------------------------

// aggregateFromDisk reads the racers' toy aggregate: a JSON object of
// key -> value. It is not jsonc and it is not Devin's real schema, because what
// is under test is the SERIALIZATION of the read-modify-commit, not the patcher.
func aggregateFromDisk(abs string) (map[string]string, []byte, error) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, err
		}
		return map[string]string{}, nil, nil
	}
	current := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &current); err != nil {
			return nil, nil, fmt.Errorf("aggregate at %s is not the object this fixture writes: %w", abs, err)
		}
	}
	return current, raw, nil
}

func aggregateValue(key string) string { return "value-" + key }

func aggregateEntries(content map[string]string) []Entry {
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]Entry, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, mcpEntry(k, "resource-"+k, content[k]))
	}
	return entries
}

// applyAddingKey is the shape of the real defect: read the aggregate, read the
// record, patch, commit. Unserialized, two of these lose one another's key or
// commit a record that no longer describes the file.
//
// The read must be inside the per-root lock because Apply's compare-and-swap
// precondition compares the root ownership generation the caller read against
// the live record at rename time. If the caller already holds the lock (the
// session pattern B3 uses), the read and Apply are one reentrant body. If not,
// applyAddingKey opens a write session around the whole read-modify-commit so
// tests that exercise Apply directly still read the right generation.
func applyAddingKey(st *Store, rel, abs, key string) (Result, error) {
	body := func() (Result, error) {
		content, raw, err := aggregateFromDisk(abs)
		if err != nil {
			return Result{}, err
		}
		content[key] = aggregateValue(key)
		// encoding/json sorts map keys, so the bytes are deterministic.
		next, err := json.MarshalIndent(content, "", "  ")
		if err != nil {
			return Result{}, err
		}
		next = append(next, '\n')
		proof, _ := st.Proof(rel)
		gen, priorDigest := proof.Generation(), ""
		if raw != nil {
			priorDigest = Digest(raw)
		}
		return st.Apply(Mutation{
			Rel:                   rel,
			AbsFile:               abs,
			NextBytes:             next,
			Entries:               aggregateEntries(content),
			Reason:                ReasonSync,
			ExpectPriorGeneration: gen,
			ExpectPriorDigest:     priorDigest,
		})
	}
	if st.HoldsLock() {
		return body()
	}
	var result Result
	err := st.WithWriteSession("apply", func() error {
		var applyErr error
		result, applyErr = body()
		return applyErr
	})
	return result, err
}

// recordDescribesFile is the invariant every writer must leave true: the durable
// record describes the bytes that are live. It asserts properties rather than
// sampling for a symptom, so it holds for any interleaving a correct
// implementation can produce.
func recordDescribesFile(st *Store, rel, abs string) error {
	content, raw, err := aggregateFromDisk(abs)
	if err != nil {
		return err
	}
	proof, err := st.Proof(rel)
	if err != nil {
		return fmt.Errorf("proof: %w", err)
	}
	if got, want := proof.ContentDigest(), Digest(raw); got != want {
		return fmt.Errorf("the record describes content %s but the live file is %s", got, want)
	}
	if got, want := proof.OwnedCount(), len(content); got != want {
		return fmt.Errorf("the record claims %d owned entries, the file has %d", got, want)
	}
	for key, value := range content {
		id := mcpEntry(key, "resource-"+key, value).ID()
		if status := proof.Classify(id, Digest([]byte(value))); status != StatusOwnedClean {
			return fmt.Errorf("entry %q classifies %s, so the next sync would treat a committed write as drift", key, status)
		}
	}
	journal, err := st.ReadJournal()
	if err != nil {
		return fmt.Errorf("read journal: %w", err)
	}
	if journal != nil {
		return fmt.Errorf("a completed generation left an in-flight journal for %s", journal.File)
	}
	staged, err := filepath.Glob(abs + ".am-staged-*")
	if err != nil {
		return err
	}
	if len(staged) != 0 {
		return fmt.Errorf("staged files survived: %v", staged)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Apply: mutual exclusion in one process, proven by the sentinel
// ---------------------------------------------------------------------------

// TestApplySectionsNeverOverlapAcrossStoresInOneProcess races several writers
// through Apply with the sentinel installed in every one of them.
//
// Each racer gets its OWN Store, which is what makes this a test of the
// mechanism rather than of an in-process mutex: a flock belongs to the open file
// description, so N Stores hold N descriptions and contend in the kernel. It is
// also the shape a caller MUST use — one Store shared by two goroutines gives
// them no mutual exclusion at all, because the second acquisition is only a
// depth bump (TestAdversarialOneStoreSharedByTwoGoroutinesIsNotMutualExclusion).
//
// Every racer writes a DIFFERENT aggregate file of the SAME root, which is the
// harder case and the reason the lock is per root rather than per file: the
// journal is one slot per root and Record.Generation is one counter covering all
// of the root's files, so two writers on two files collide exactly as hard as two
// on one. Unserialized, both read generation N, both commit N+1, and the
// surviving record silently drops one file's FileState.
func TestApplySectionsNeverOverlapAcrossStoresInOneProcess(t *testing.T) {
	f := newFixture(t)
	sentinel := filepath.Join(t.TempDir(), "inflight")
	t.Cleanup(func() { _ = os.Remove(sentinel) })

	const racers = 6
	start := make(chan struct{})
	errs := make(chan error, racers)
	gens := make(chan uint64, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		rel := fmt.Sprintf("aggregate-%d.json", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A real clock: the fixture's deterministic one is a plain field and
			// these racers genuinely run in parallel. One Store per racer.
			st := f.store(
				WithClock(time.Now),
				WithLockTimeout(60*time.Second),
				WithCrasher(&sectionSentinel{path: sentinel}),
			)
			<-start
			res, err := applyAddingKey(st, rel, filepath.Join(f.targetRoot, rel), "server")
			errs <- err
			gens <- res.Generation
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(gens)

	for err := range errs {
		if err != nil {
			t.Fatalf("racer: %v", err)
		}
	}
	// Generations are consecutive with no gaps and no reuse.
	seen := make([]uint64, 0, racers)
	for g := range gens {
		seen = append(seen, g)
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i] < seen[j] })
	for i, g := range seen {
		if g != uint64(i+1) {
			t.Fatalf("generations %v are not consecutive from 1", seen)
		}
	}
	if _, err := os.Lstat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the sentinel outlived every section: %v", err)
	}
	// Not one file's ownership was dropped from the shared record.
	final := f.store()
	for i := 0; i < racers; i++ {
		rel := fmt.Sprintf("aggregate-%d.json", i)
		if err := recordDescribesFile(final, rel, filepath.Join(f.targetRoot, rel)); err != nil {
			t.Fatalf("after %d serialized generations, %s: %v", racers, rel, err)
		}
	}
}

// TestApplySentinelHarnessReportsAnOverlap is the harness's own positive
// control, kept in the tree permanently. It proves the instrument REPORTS an
// overlap rather than passing quietly, without depending on any schedule: the
// test creates the sentinel itself, so Apply walks into a section another writer
// is notionally already inside.
//
// Without it, a silently broken sentinel would make every test above pass
// vacuously.
func TestApplySentinelHarnessReportsAnOverlap(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	sentinel := filepath.Join(t.TempDir(), "inflight")
	t.Cleanup(func() { _ = os.Remove(sentinel) })
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	st := f.store(WithCrasher(&sectionSentinel{path: sentinel}))
	_, err := applyAddingKey(st, f.rel, f.absFile, "server-0")
	if err == nil {
		t.Fatal("the sentinel harness did not report an occupied critical section; every test that relies on it is vacuous")
	}
	if !strings.Contains(err.Error(), "overlapped") {
		t.Fatalf("overlap error = %v, want the harness's own message", err)
	}
	// And the refused mutation wrote nothing: the harness fires before the
	// mandatory safety backup.
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("aggregate changed: %q", content)
	}
	if fileExists(t, st.StateFile()) {
		t.Fatal("a mutation refused at the first crash point committed a record")
	}
}

// ---------------------------------------------------------------------------
// Apply: mutual exclusion across real processes
// ---------------------------------------------------------------------------

const (
	applyChildModeEnv     = "AM_OWNERSHIP_APPLY_CHILD_MODE"
	applyChildKeyEnv      = "AM_OWNERSHIP_APPLY_CHILD_KEY"
	applyChildRelEnv      = "AM_OWNERSHIP_APPLY_CHILD_REL"
	applyChildRootEnv     = "AM_OWNERSHIP_APPLY_CHILD_TARGET_ROOT"
	applyChildStateEnv    = "AM_OWNERSHIP_APPLY_CHILD_STATE_ROOT"
	applyChildSentinelEnv = "AM_OWNERSHIP_APPLY_CHILD_SENTINEL"

	// childModeApply calls Apply directly, with NO outer section, so the only
	// thing that can serialize it is Apply's own acquisition. This is the mode
	// that would go red if that acquisition were removed.
	childModeApply = "apply"
	// childModeSession is the caller pattern B3 will use in
	// internal/materialize: one explicit session around the read, the patch, and
	// the commit, with Apply's own acquisition reentering it.
	childModeSession = "session"

	applyChildReady      = "READY"
	applyChildGeneration = "GENERATION"
)

// TestApplyChildProcess is not a test: it is the body of the child process the
// cross-process tests exec, inert unless its mode env var is set. It performs one
// real journaled generation against the root it is given, with the O_EXCL
// sentinel installed inside Apply, and reports the generation it committed on
// stdout.
func TestApplyChildProcess(t *testing.T) {
	mode := os.Getenv(applyChildModeEnv)
	if mode == "" {
		t.Skip("not a child process invocation")
	}
	targetRoot := os.Getenv(applyChildRootEnv)
	st, err := Open(targetRoot,
		WithStateRoot(os.Getenv(applyChildStateEnv)),
		WithLockTimeout(60*time.Second),
		WithCrasher(&sectionSentinel{path: os.Getenv(applyChildSentinelEnv)}),
	)
	if err != nil {
		t.Fatalf("child Open: %v", err)
	}
	rel := os.Getenv(applyChildRelEnv)
	abs := filepath.Join(targetRoot, rel)
	key := os.Getenv(applyChildKeyEnv)

	// Announce readiness and block on a pipe read until the parent releases every
	// sibling at once. Without that rendezvous the children would reach their
	// sections tens of milliseconds apart — process startup dominates one
	// generation — and could never overlap even with no locking at all, so the
	// sentinel would prove nothing.
	fmt.Println(applyChildReady)
	_, _ = os.Stdin.Read(make([]byte, 1))

	var res Result
	switch mode {
	case childModeApply:
		res, err = applyAddingKey(st, rel, abs, key)
		if err != nil {
			t.Fatalf("child Apply: %v", err)
		}
		// The record must describe the file. The observation is taken inside a
		// fresh section, because that is the only way to read the file and the
		// record as a PAIR: a lock-free reader is allowed to see a record one
		// generation behind the file, and the design says so explicitly.
		if err := st.WithLock(func() error { return recordDescribesFile(st, rel, abs) }); err != nil {
			t.Fatalf("child observation after generation %d: %v", res.Generation, err)
		}
	case childModeSession:
		err = st.WithLock(func() error {
			var applyErr error
			res, applyErr = applyAddingKey(st, rel, abs, key)
			if applyErr != nil {
				return applyErr
			}
			return recordDescribesFile(st, rel, abs)
		})
		if err != nil {
			t.Fatalf("child session: %v", err)
		}
	default:
		t.Fatalf("unknown child mode %q", mode)
	}
	fmt.Println(applyChildGeneration, res.Generation)
}

func applyChildCmd(t *testing.T, mode, key, rel, targetRoot, stateRoot, sentinel string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplyChildProcess$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(),
		applyChildModeEnv+"="+mode,
		applyChildKeyEnv+"="+key,
		applyChildRelEnv+"="+rel,
		applyChildRootEnv+"="+targetRoot,
		applyChildStateEnv+"="+stateRoot,
		applyChildSentinelEnv+"="+sentinel,
	)
	return cmd
}

// applyChild is one started, rendezvoused child.
type applyChild struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
}

// startApplyChildren execs one child per spec and returns once every one of them
// has announced, on a pipe, that it is parked immediately before its generation.
func startApplyChildren(t *testing.T, mode string, f *fixture, sentinel string, specs [][2]string) []*applyChild {
	t.Helper()
	children := make([]*applyChild, 0, len(specs))
	for _, spec := range specs {
		rel, key := spec[0], spec[1]
		cmd := applyChildCmd(t, mode, key, rel, f.targetRoot, f.stateRoot, sentinel)
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatalf("child %s stdin: %v", rel, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("child %s stdout: %v", rel, err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %s: %v", rel, err)
		}
		// A parked child blocks on a pipe read, so a t.Fatal anywhere below would
		// otherwise leave it alive until its own -test.timeout expires.
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		scanner := bufio.NewScanner(stdout)
		if !scanLineContaining(scanner, applyChildReady) {
			t.Fatalf("child %s never reported readiness", rel)
		}
		children = append(children, &applyChild{name: rel + "/" + key, cmd: cmd, stdin: stdin, scanner: scanner})
	}
	return children
}

// releaseAndCollect releases every child at once and returns the generations they
// committed.
func releaseAndCollect(t *testing.T, children []*applyChild) []uint64 {
	t.Helper()
	for _, c := range children {
		_, _ = c.stdin.Write([]byte("\n"))
		_ = c.stdin.Close()
	}
	generations := make([]uint64, 0, len(children))
	for _, c := range children {
		gen, ok := scanGeneration(c.scanner)
		if !ok {
			t.Errorf("child %s reported no generation", c.name)
			continue
		}
		generations = append(generations, gen)
	}
	for _, c := range children {
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("child %s failed; an overlapped section or a record that stopped describing the file is reported above: %v", c.name, err)
		}
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i] < generations[j] })
	return generations
}

// TestApplyFromTwoProcessesProducesSequentialGenerations is the acceptance test
// for this bead, and the one an in-process test cannot be: the writers are real
// OS processes, so a "fix" implemented as a process-global mutex keyed by root
// fails here. Both racers are released from a pipe barrier, neither opens a
// section of its own, and both carry the sentinel — so an overlap is reported by
// the kernel rather than inferred, and the only thing that can serialize them is
// Apply's own acquisition.
//
// They write two DIFFERENT aggregate files of the same root, because the journal
// is one slot per root and Record.Generation is one counter for all of the root's
// files: unserialized, both read generation N, both commit N+1, and the surviving
// record drops one file's ownership entirely.
func TestApplyFromTwoProcessesProducesSequentialGenerations(t *testing.T) {
	f := newFixture(t)
	sentinel := filepath.Join(t.TempDir(), "inflight")
	t.Cleanup(func() { _ = os.Remove(sentinel) })
	specs := [][2]string{{"mcp_config.json", "alpha"}, {"hooks.v1.json", "beta"}}

	children := startApplyChildren(t, childModeApply, f, sentinel, specs)
	generations := releaseAndCollect(t, children)

	if want := []uint64{1, 2}; len(generations) != 2 || generations[0] != want[0] || generations[1] != want[1] {
		t.Fatalf("two processes committed generations %v, want sequential %v", generations, want)
	}
	if _, err := os.Lstat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the sentinel outlived both sections: %v", err)
	}

	// Both files' ownership survived in the one shared record, and each record
	// entry describes the bytes that are live.
	st := f.store()
	for _, spec := range specs {
		rel := spec[0]
		if err := recordDescribesFile(st, rel, filepath.Join(f.targetRoot, rel)); err != nil {
			t.Fatalf("after two cross-process generations, %s: %v", rel, err)
		}
	}
}

// TestSessionsFromTwoProcessesNeitherLoseAnUpdate covers the caller pattern B3
// installs in internal/materialize: the READ of the aggregate is inside the
// section too, so the whole read-modify-commit is atomic across processes.
//
// Both processes patch the SAME file, which is the one shape a lock inside Apply
// alone cannot save — Apply cannot serialize a read its caller performed before
// calling it — so this test states exactly which guarantee belongs to which
// section boundary, and it is the regression guard for B3 widening it.
func TestSessionsFromTwoProcessesNeitherLoseAnUpdate(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	sentinel := filepath.Join(t.TempDir(), "inflight")
	t.Cleanup(func() { _ = os.Remove(sentinel) })
	specs := [][2]string{{f.rel, "alpha"}, {f.rel, "beta"}}

	children := startApplyChildren(t, childModeSession, f, sentinel, specs)
	generations := releaseAndCollect(t, children)

	if want := []uint64{1, 2}; len(generations) != 2 || generations[0] != want[0] || generations[1] != want[1] {
		t.Fatalf("two processes committed generations %v, want sequential %v", generations, want)
	}
	if _, err := os.Lstat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("the sentinel outlived both sections: %v", err)
	}

	st := f.store()
	if err := recordDescribesFile(st, f.rel, f.absFile); err != nil {
		t.Fatalf("after two cross-process sessions: %v", err)
	}
	content, _, err := aggregateFromDisk(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if _, ok := content[spec[1]]; !ok {
			t.Fatalf("key %q is missing from the aggregate %v; one writer's read was overtaken by the other's commit", spec[1], content)
		}
	}
}

func scanGeneration(scanner *bufio.Scanner) (uint64, bool) {
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || fields[0] != applyChildGeneration {
			continue
		}
		gen, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return gen, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Each writer entry point is genuinely inside the section
// ---------------------------------------------------------------------------

// TestApplyIsRefusedWhileAnotherProcessHoldsTheLock manufactures the contention
// instead of racing for it: a real child process is provably inside the section
// (it said so on a pipe) when Apply is attempted with a fail-fast timeout.
//
// It is the deterministic proof that Apply acquires at all — on a tree with the
// acquisition removed, the Apply below simply succeeds.
func TestApplyIsRefusedWhileAnotherProcessHoldsTheLock(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	_, letGo := startHolder(t, f.targetRoot, f.stateRoot)

	st := f.store(WithLockTimeout(0))
	if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Apply while another process held the root lock: want ErrLockTimeout, got %v", err)
	}
	// A refused acquisition leaves the root exactly as found — including the
	// state root, so not even a safety backup was burned.
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("a refused Apply rewrote the aggregate: %q", content)
	}
	if fileExists(t, st.StateFile()) {
		t.Fatal("a refused Apply committed an ownership record")
	}
	if fileExists(t, st.SafetyBackupDir(f.rel)) {
		t.Fatal("a refused Apply took a safety backup")
	}

	letGo()

	// And the refusal was contention, not a broken write path.
	if _, err := applyAddingKey(f.store(), f.rel, f.absFile, "alpha"); err != nil {
		t.Fatalf("Apply after the holder released: %v", err)
	}
}

// TestRecoverCannotResolveAnotherProcessesLiveJournal is the essential one.
//
// Recover is a WRITER: rollForward commits a record, and rollBack and abandon
// delete the staged file and clear the journal. Unserialized, process B's
// Recover can abandon process A's genuinely in-flight generation. Here process A
// is a real child holding the lock while a live, phase-"prepared" journal and its
// staged file sit on disk; B's Recover must refuse rather than resolve them.
func TestRecoverCannotResolveAnotherProcessesLiveJournal(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	// Manufacture the in-flight generation with the production crash seam: a
	// journal in phase "prepared" plus a complete staged file.
	crashed := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	if _, err := applyAddingKey(crashed, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("seeding an in-flight generation: want ErrInjectedCrash, got %v", err)
	}
	journalBefore, err := readFile(crashed.JournalFile())
	if err != nil {
		t.Fatalf("the fixture wrote no journal, so this test would prove nothing: %v", err)
	}
	staged, err := filepath.Glob(f.absFile + ".am-staged-*")
	if err != nil || len(staged) != 1 {
		t.Fatalf("staged files = %v (err %v), want exactly one", staged, err)
	}

	_, letGo := startHolder(t, f.targetRoot, f.stateRoot)

	st := f.store(WithLockTimeout(0))
	if _, err := st.Recover(); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Recover while another process held the root lock: want ErrLockTimeout, got %v", err)
	}
	if got, err := readFile(st.JournalFile()); err != nil || got != journalBefore {
		t.Fatalf("a refused Recover changed another process's live journal (err %v):\n%s", err, got)
	}
	if !fileExists(t, staged[0]) {
		t.Fatal("a refused Recover deleted the staged file out from under the holder")
	}
	if fileExists(t, st.StateFile()) {
		t.Fatal("a refused Recover committed an ownership generation")
	}

	letGo()

	// Once the holder is gone the same call resolves the journal normally: the
	// aggregate is still the pre-mutation content, so this rolls BACK.
	outcomes, err := f.store().Recover()
	if err != nil {
		t.Fatalf("Recover after the holder released: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Fatalf("recovery outcomes = %+v, want one rolled-back", outcomes)
	}
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("rollback touched the live file: %q", content)
	}
}

// TestBackupOnlyIsRefusedWhileAnotherProcessHoldsTheLock: BackupOnly writes —
// it creates the state directory, retains bytes, stamps a generation it read
// from Load, and prunes older backups — so it is inside the section too.
func TestBackupOnlyIsRefusedWhileAnotherProcessHoldsTheLock(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{ this is not json")

	_, letGo := startHolder(t, f.targetRoot, f.stateRoot)

	st := f.store(WithLockTimeout(0))
	if _, err := st.BackupOnly(f.rel, f.absFile, ReasonSync); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("BackupOnly while another process held the root lock: want ErrLockTimeout, got %v", err)
	}
	if fileExists(t, st.SafetyBackupDir(f.rel)) {
		t.Fatal("a refused BackupOnly retained bytes anyway")
	}
	if st.EverRecorded() {
		t.Fatal("a refused BackupOnly left durable evidence that this root was managed")
	}

	letGo()

	path, err := f.store().BackupOnly(f.rel, f.absFile, ReasonSync)
	if err != nil {
		t.Fatalf("BackupOnly after the holder released: %v", err)
	}
	got, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "{ this is not json" {
		t.Fatalf("retained bytes = %q, want the exact unparseable original", got)
	}
}

// ---------------------------------------------------------------------------
// Acquisition happens BEFORE validation, and a refusal still writes nothing
// ---------------------------------------------------------------------------

// TestApplyAcquiresBeforeValidatingAndARefusalWritesNothing pins both halves of
// the boundary decision.
//
// The lock is taken before [validateMutation], so even a refused mutation is
// serialized — the refusal is decided against the same state the mutation would
// have been made against. And the refusal still writes NOTHING: no aggregate
// change, no safety backup, no record, no ownership directory. That last part is
// what keeps a first-ever run silent, because Store.EverRecorded reads the
// ownership and safety-backup directories as durable evidence and acquisition
// must never create either (the lock lives in a sibling locks/ directory).
func TestApplyAcquiresBeforeValidatingAndARefusalWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")

	var mu sync.Mutex
	var kinds []LockEventKind
	st := f.store(WithLockObserver(func(ev LockEvent) {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, ev.Kind)
	}))

	// An UNCONFIRMED destructive mutation: validateMutation refuses it.
	_, err := st.Apply(Mutation{
		Rel:     f.rel,
		AbsFile: f.absFile,
		Entries: nil,
		Reason:  ReasonOrphanRemoval,
	})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed orphan removal: want ErrConfirmationRequired, got %v", err)
	}

	mu.Lock()
	observed := append([]LockEventKind(nil), kinds...)
	mu.Unlock()
	acquired, released := 0, 0
	for _, k := range observed {
		switch k {
		case LockAcquired:
			acquired++
		case LockReleased:
			released++
		}
	}
	if acquired != 1 {
		t.Fatalf("a refused mutation acquired the lock %d times, want 1: acquisition must precede validateMutation, got events %v", acquired, observed)
	}
	if released != 1 {
		t.Fatalf("a refused mutation released the lock %d times, want 1", released)
	}
	if st.HoldsLock() {
		t.Fatal("a refused mutation kept the lock")
	}

	// Nothing was written, anywhere.
	if content, _ := f.readAggregate(); content != "{}\n" {
		t.Fatalf("a refused mutation rewrote the aggregate: %q", content)
	}
	if fileExists(t, st.StateFile()) || fileExists(t, st.JournalFile()) {
		t.Fatal("a refused mutation wrote ownership state")
	}
	if fileExists(t, filepath.Join(st.StateRoot(), ownershipDirName, st.RootKey())) {
		t.Fatal("a refused mutation created the ownership record directory")
	}
	if fileExists(t, st.SafetyBackupDir(f.rel)) {
		t.Fatal("a refused mutation took a safety backup")
	}

	// And a first-ever run therefore still reports the silent flavour of "no
	// record" rather than a user-facing ownership.state-missing warning.
	if st.EverRecorded() {
		t.Fatal("a refused mutation made EverRecorded answer yes")
	}
	if _, err := st.Load(); !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Load after a refused mutation: want ErrStateUninitialized, got %v", err)
	}
	if _, err := st.Proof(f.rel); !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Proof after a refused mutation: want ErrStateUninitialized, got %v", err)
	}
	// The lock file itself was created — otherwise the assertions above would be
	// vacuous — and it is a sibling of every evidence directory.
	if !fileExists(t, st.LockPath()) {
		t.Fatal("no lock file was created, so this test does not prove acquisition happened")
	}
}

// TestWithWriteLockRunsTheBodyExactlyOnce pins the exactly-once contract of
// Store.withWriteLock on all four of its paths, including the one no writer can
// reach through Apply, Recover, or BackupOnly today: a BODY that returns an error
// wrapping ErrLockUnsupported.
//
// That path is why the `started` flag exists. Without it the degraded branch
// cannot tell "acquisition said unsupported" from "the mutation failed and its
// error happens to wrap ErrLockUnsupported", so it would run the mutation a
// SECOND time — a second generation and a second safety backup. The three
// production callers cannot produce such an error, which is exactly why only a
// direct test can keep the guarantee from silently becoming a coincidence.
func TestWithWriteLockRunsTheBodyExactlyOnce(t *testing.T) {
	f := newFixture(t)
	unsupported := func(*os.File) error { return ErrLockUnsupported }

	for _, tc := range []struct {
		name     string
		setup    func(st *Store) func()
		bodyErr  error
		wantRuns int
		wantErr  error
	}{
		{name: "lock held, body succeeds", wantRuns: 1},
		{
			name:     "lock held, body error wraps ErrLockUnsupported",
			bodyErr:  fmt.Errorf("the mutation failed: %w", ErrLockUnsupported),
			wantRuns: 1,
			wantErr:  ErrLockUnsupported,
		},
		{
			name:     "acquisition unsupported, so the body degrades",
			setup:    func(st *Store) func() { st.tryLock = unsupported; return func() {} },
			wantRuns: 1,
		},
		{
			name: "acquisition times out, so the body never runs",
			setup: func(st *Store) func() {
				holder := f.store()
				release, err := holder.Lock()
				if err != nil {
					t.Fatalf("holder Lock: %v", err)
				}
				st.lockTimeout = 0
				return release
			},
			wantRuns: 0,
			wantErr:  ErrLockTimeout,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := f.store()
			if tc.setup != nil {
				defer tc.setup(st)()
			}
			runs := 0
			err := st.withWriteLock("test", func() error {
				runs++
				return tc.bodyErr
			})
			if runs != tc.wantRuns {
				t.Fatalf("the body ran %d times, want %d", runs, tc.wantRuns)
			}
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("withWriteLock: %v", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("withWriteLock error = %v, want one wrapping %v", err, tc.wantErr)
			}
			if st.HoldsLock() {
				t.Fatal("withWriteLock returned still holding the lock")
			}
		})
	}
}

// TestApplyStillTakesTheMandatorySafetyBackupBeforeMutating pins that widening
// the section changed no ordering INSIDE it: the backup is taken before the
// journal, the staged write, the rename, and the record.
func TestApplyStillTakesTheMandatorySafetyBackupBeforeMutating(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\"handwritten\":\"keep me\"}\n")

	// Crash at the first point after the backup: whatever exists on disk now was
	// written before any mutation.
	st := f.store(WithCrasher(CrashAt(CrashAfterBackup)))
	if _, err := applyAddingKey(st, f.rel, f.absFile, "alpha"); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("want ErrInjectedCrash, got %v", err)
	}
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("retained backups = %d, want 1 taken before the mutation", len(backups))
	}
	raw, err := backups[0].RawBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{\"handwritten\":\"keep me\"}\n" {
		t.Fatalf("the backup does not hold the exact pre-mutation bytes: %q", raw)
	}
	if content, _ := f.readAggregate(); content != "{\"handwritten\":\"keep me\"}\n" {
		t.Fatalf("the aggregate was mutated before the backup: %q", content)
	}
	if fileExists(t, st.JournalFile()) {
		t.Fatal("a journal exists at the after-backup crash point")
	}
}

// ---------------------------------------------------------------------------
// Reads take no lock
// ---------------------------------------------------------------------------

// TestReadPathsTakeNoLockWhileAWriterHoldsIt is the other half of the design:
// every read is a single os.ReadFile of a file only ever replaced by rename, so
// the worst view a reader can get is one generation behind — which classifies
// Unmanaged or OwnedDrifted, both preserve-don't-prune. Locking reads would
// instead break Open's documented side-effect-free contract that `status` and
// `sync --check` depend on, and would make a read-only command block behind a
// long-running sync.
//
// The reader here is configured with the DEFAULT timeout, so a read that took
// the lock would block for 30s and then fail rather than flake.
func TestReadPathsTakeNoLockWhileAWriterHoldsIt(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	if _, err := applyAddingKey(f.store(), f.rel, f.absFile, "alpha"); err != nil {
		t.Fatalf("seed generation: %v", err)
	}

	// A different Store holds the lock, so it genuinely excludes other Stores.
	holder := f.store()
	releaseHolder, err := holder.Lock()
	if err != nil {
		t.Fatalf("holder Lock: %v", err)
	}
	defer releaseHolder()

	before := stateRootSnapshot(t, f.stateRoot)
	reader := f.store() // default 30s timeout: blocking would be unmissable

	deadline := time.Now().Add(10 * time.Second)
	rec, err := reader.Load()
	if err != nil {
		t.Fatalf("Load while a writer holds the lock: %v", err)
	}
	proof, err := reader.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof while a writer holds the lock: %v", err)
	}
	if proof.OwnedCount() != 1 {
		t.Fatalf("proof covers %d entries, want 1", proof.OwnedCount())
	}
	id := mcpEntry("alpha", "resource-alpha", aggregateValue("alpha")).ID()
	if status := proof.Classify(id, Digest([]byte(aggregateValue("alpha")))); status != StatusOwnedClean {
		t.Fatalf("Classify under a held lock = %s, want owned-clean", status)
	}
	if !reader.EverRecorded() {
		t.Fatal("EverRecorded = false after a committed generation")
	}
	if _, err := reader.SafetyBackups(f.rel); err != nil {
		t.Fatalf("SafetyBackups while a writer holds the lock: %v", err)
	}
	if _, err := reader.ReadJournal(); err != nil {
		t.Fatalf("ReadJournal while a writer holds the lock: %v", err)
	}
	if _, err := reader.OrphanCandidates(proof, f.rel, nil, nil); err != nil {
		t.Fatalf("OrphanCandidates while a writer holds the lock: %v", err)
	}
	if time.Now().After(deadline) {
		t.Fatal("the read paths blocked; one of them is waiting on the per-root lock")
	}
	if rec == nil || rec.Generation != 1 {
		t.Fatalf("record = %+v, want generation 1", rec)
	}

	// No read acquired anything, and no read created anything under the state
	// root — which is what keeps `status` and `sync --check` side-effect free.
	if reader.HoldsLock() || reader.LockDepth() != 0 {
		t.Fatalf("a read path acquired the lock: depth %d", reader.LockDepth())
	}
	if after := stateRootSnapshot(t, f.stateRoot); after != before {
		t.Fatalf("read paths changed the state root:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestReadPathsOnAFirstEverRunCreateNothingAndStaySilent is the `status` /
// `sync --check` shape on a machine that has never synced: the reads must report
// the silent first-use flavour and must not create the state root at all. Bead
// agent-marketplace-5ew4 depends on this.
func TestReadPathsOnAFirstEverRunCreateNothingAndStaySilent(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\"handwritten\":\"keep me\"}\n")
	st := f.store()

	if _, err := st.Load(); !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Load on a first-ever run: want ErrStateUninitialized, got %v", err)
	}
	proof, err := st.Proof(f.rel)
	if !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("Proof on a first-ever run: want ErrStateUninitialized, got %v", err)
	}
	if errors.Is(err, ErrLockTimeout) || errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("a read path reported a locking failure: %v", err)
	}
	// Fail closed: every existing entry is unmanaged, so sync preserves
	// everything and prunes nothing.
	id := mcpEntry("handwritten", "resource-handwritten", "keep me").ID()
	if status := proof.Classify(id, Digest([]byte("keep me"))); status != StatusUnmanaged {
		t.Fatalf("Classify with no record = %s, want unmanaged", status)
	}
	if st.EverRecorded() {
		t.Fatal("EverRecorded = true on a first-ever run")
	}
	if _, err := os.Lstat(f.stateRoot); !os.IsNotExist(err) {
		t.Fatalf("a read-only pass created the state root: %v", err)
	}
}

// stateRootSnapshot renders every path under the state root with its size, so a
// test can assert that a read-only pass changed nothing at all.
func stateRootSnapshot(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		fmt.Fprintf(&sb, "%s %d\n", rel, info.Size())
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return sb.String()
}
