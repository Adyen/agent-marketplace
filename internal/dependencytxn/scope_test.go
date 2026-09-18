package dependencytxn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func scopeStateDir(t *testing.T) string {
	t.Helper()
	_, state := testDirs(t)
	return state
}

func scopedDesired(t *testing.T, path string, data []byte) DesiredFile {
	t.Helper()
	generation, err := ReadGeneration(path)
	if err != nil {
		t.Fatal(err)
	}
	return DesiredFile{Path: path, Expected: generation, Bytes: data, Mode: 0o600}
}

func TestCommitScopeEnclosesPublicationAndLocks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readLock bool
	}{
		{name: "desired file"},
		{name: "read assertion", readLock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			stateDir := scopeStateDir(t)
			desired := []DesiredFile{scopedDesired(t, path, []byte("new"))}
			canonicalPath, err := CanonicalConfigDestination(path)
			if err != nil {
				t.Fatal(err)
			}
			var options []Option
			if tc.readLock {
				generation, err := ReadGeneration(path)
				if err != nil {
					t.Fatal(err)
				}
				options = append(options, WithReadAssertions([]ReadAssertion{{Path: canonicalPath, RequestedPath: path, Expected: generation}}))
			}
			options = append(options, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
				if raw, err := os.ReadFile(path); err != nil || string(raw) != "old" {
					return Result{}, fmt.Errorf("before publish = %q: %w", raw, err)
				}
				result, err := publish()
				if err != nil {
					return result, err
				}
				if raw, err := os.ReadFile(path); err != nil || string(raw) != "new" {
					return result, fmt.Errorf("after publish = %q: %w", raw, err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				var contenderErr error
				if tc.readLock {
					generation, readErr := ReadGeneration(canonicalPath)
					if readErr != nil {
						return result, readErr
					}
					_, contenderErr = Commit(ctx, stateDir, nil, WithReadAssertions([]ReadAssertion{{Path: canonicalPath, RequestedPath: path, Expected: generation}}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) { return publish() }))
				} else {
					_, contenderErr = Commit(ctx, stateDir, []DesiredFile{scopedDesired(t, path, []byte("contender"))})
				}
				if !errors.Is(contenderErr, ErrLockTimeout) || !errors.Is(contenderErr, context.DeadlineExceeded) {
					return result, fmt.Errorf("contender error = %w", contenderErr)
				}
				return Result{}, nil
			}))
			result, err := Commit(context.Background(), stateDir, desired, options...)
			if err != nil || len(result.Changed) != 1 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if tc.readLock {
				generation, err := ReadGeneration(canonicalPath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = Commit(context.Background(), stateDir, nil, WithReadAssertions([]ReadAssertion{{Path: canonicalPath, RequestedPath: path, Expected: generation}}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) { return publish() }))
				if err != nil {
					t.Fatal(err)
				}
			} else if _, err := Commit(context.Background(), stateDir, []DesiredFile{scopedDesired(t, path, []byte("contender"))}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommitScopeRejectsRetainedPublish(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("scopeError=%t", fail), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			state := scopeStateDir(t)
			sentinel := errors.New("preflight failed")
			var retained func() (Result, error)
			_, err := Commit(context.Background(), state, []DesiredFile{scopedDesired(t, path, []byte("new"))}, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
				retained = publish
				if fail {
					return Result{}, sentinel
				}
				return Result{}, nil
			}))
			if err == nil || retained == nil || (fail && !errors.Is(err, sentinel)) {
				t.Fatalf("retained=%t error=%v", retained != nil, err)
			}
			result, err := retained()
			if !errors.Is(err, ErrCommitScopeClosed) || len(result.Changed) != 0 {
				t.Fatalf("retained result=%#v error=%v", result, err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("closed callback published destination: %v", err)
			}
		})
	}
}

func TestCommitScopePreflightAndCommittedFailure(t *testing.T) {
	t.Run("preflight", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		sentinel := errors.New("preflight")
		_, err := Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, path, []byte("new"))}, WithCommitScope(func(context.Context, func() (Result, error)) (Result, error) {
			return Result{}, sentinel
		}))
		var committed *CommittedScopeError
		if !errors.Is(err, sentinel) || errors.As(err, &committed) {
			t.Fatalf("error = %v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("destination changed: %v", err)
		}
	})
	t.Run("post commit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		sentinel := errors.New("cleanup")
		result, err := Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, path, []byte("new"))}, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			result, err := publish()
			if err != nil {
				return result, err
			}
			return result, sentinel
		}))
		var committed *CommittedScopeError
		if !errors.As(err, &committed) || !errors.Is(err, sentinel) || len(result.Changed) != 1 {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "new" {
			t.Fatalf("destination=%q error=%v", raw, err)
		}
	})
}

func TestCommitScopeNoopZeroDoubleAndMissingPublish(t *testing.T) {
	t.Run("zero desired", func(t *testing.T) {
		called := 0
		_, err := Commit(context.Background(), scopeStateDir(t), nil, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			called++
			return publish()
		}))
		if err != nil || called != 1 {
			t.Fatalf("called=%d error=%v", called, err)
		}
	})
	t.Run("no change", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
			t.Fatal(err)
		}
		called := 0
		_, err := Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, path, []byte("same"))}, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			called++
			return publish()
		}))
		if err != nil || called != 1 {
			t.Fatalf("called=%d error=%v", called, err)
		}
	})
	t.Run("double", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		_, err := Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, path, []byte("new"))}, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			if _, err := publish(); err != nil {
				return Result{}, err
			}
			return publish()
		}))
		var committed *CommittedScopeError
		if !errors.As(err, &committed) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("swallowed publish failure", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		desired := scopedDesired(t, path, []byte("new"))
		desired.Expected = Generation{Exists: true, Digest: Digest([]byte("other"))}
		_, err := Commit(context.Background(), scopeStateDir(t), []DesiredFile{desired}, WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			_, _ = publish()
			return Result{}, nil
		}))
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("missing publish", func(t *testing.T) {
		_, err := Commit(context.Background(), scopeStateDir(t), nil, WithCommitScope(func(context.Context, func() (Result, error)) (Result, error) {
			return Result{}, nil
		}))
		if err == nil {
			t.Fatal("missing publish accepted")
		}
	})
}

func TestCommitScopeAbsentReadAndPostPublishValidation(t *testing.T) {
	t.Run("absent parent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing", "config.local.yaml")
		canonical, err := CanonicalConfigDestination(path)
		if err != nil {
			t.Fatal(err)
		}
		read := ReadAssertion{Path: canonical, RequestedPath: path}
		_, err = Commit(context.Background(), scopeStateDir(t), nil, WithReadAssertions([]ReadAssertion{read}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			return publish()
		}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Commit(context.Background(), scopeStateDir(t), nil, WithReadAssertions([]ReadAssertion{read}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return Result{}, err
			}
			if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
				return Result{}, err
			}
			return publish()
		}))
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("restore published destination", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "config.yaml")
		if err := os.WriteFile(path, []byte("selected"), 0o600); err != nil {
			t.Fatal(err)
		}
		canonical, err := CanonicalConfigDestination(path)
		if err != nil {
			t.Fatal(err)
		}
		generation, err := ReadGeneration(canonical)
		if err != nil {
			t.Fatal(err)
		}
		cleanup := 0
		_, err = Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, path, []byte("excluded"))}, WithReadAssertions([]ReadAssertion{{Path: canonical, RequestedPath: path, Expected: generation}}), WithFaultHook(func(point FaultPoint) error {
			if point.Stage == FaultAfterFinalize {
				return os.WriteFile(path, []byte("selected"), 0o600)
			}
			return nil
		}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			result, err := publish()
			if err == nil {
				cleanup++
			}
			return result, err
		}))
		var committed *CommittedScopeError
		if !errors.As(err, &committed) || !errors.Is(err, ErrGenerationConflict) || cleanup != 0 {
			t.Fatalf("cleanup=%d error=%v", cleanup, err)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "selected" {
			t.Fatalf("restored=%q error=%v", raw, err)
		}
	})
	t.Run("after finalize", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "config.yaml")
		input := filepath.Join(root, "include.yaml")
		if err := os.WriteFile(input, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		canonicalInput, err := CanonicalConfigDestination(input)
		if err != nil {
			t.Fatal(err)
		}
		generation, err := ReadGeneration(canonicalInput)
		if err != nil {
			t.Fatal(err)
		}
		read := ReadAssertion{Path: canonicalInput, RequestedPath: input, Expected: generation}
		cleanup := 0
		_, err = Commit(context.Background(), scopeStateDir(t), []DesiredFile{scopedDesired(t, destination, []byte("new"))}, WithReadAssertions([]ReadAssertion{read}), WithFaultHook(func(point FaultPoint) error {
			if point.Stage == FaultAfterFinalize {
				return os.WriteFile(input, []byte("changed"), 0o600)
			}
			return nil
		}), WithCommitScope(func(_ context.Context, publish func() (Result, error)) (Result, error) {
			result, err := publish()
			if err == nil {
				cleanup++
			}
			return result, err
		}))
		var committed *CommittedScopeError
		if !errors.As(err, &committed) || !errors.Is(err, ErrGenerationConflict) || cleanup != 0 {
			t.Fatalf("cleanup=%d error=%v", cleanup, err)
		}
	})
}
