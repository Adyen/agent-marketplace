package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestDiscoverTagBump(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current string
		tags    []string
		want    string
	}{
		{name: "empty", current: "v1.0"},
		{name: "not_versioned", current: "main", tags: []string{"v1", "v2"}},
		{name: "simple", current: " v1 ", tags: []string{"v1", "v2", "v10", "v3"}, want: "v10"},
		{name: "numeric_only", current: "1", tags: []string{"2", "10", "v20"}, want: "10"},
		{name: "same_family", current: "v1.0.0", tags: []string{"v1.0.2", "v1.0.10", "v1.1.0", "v2.0.0"}, want: "v1.0.10"},
		{name: "literal_prefix", current: "release/2", tags: []string{"release/3", "release/11"}, want: "release/11"},
		{name: "not_numeric", current: "v1.0", tags: []string{"v1.1-rc", "v1.2.3", "v1.other", "v1.9"}, want: "v1.9"},
		{name: "equal_current", current: "v1.2", tags: []string{"v1.2"}, want: "v1.2"},
		{name: "lower_only", current: "v1.9", tags: []string{"v1.1", "v1.8"}},
		{name: "lexical_tie", current: "v1.0", tags: []string{"v1.2", "v1.02", "v1.002"}, want: "v1.2"},
		{name: "last_unicode_space", current: "v1.0", tags: []string{"v1.2\u00a0"}, want: "v1.2"},
		{name: "long_valid_tag", current: "v1.0", tags: []string{"v1." + strings.Repeat("0", 70_000) + "2"}, want: "v1." + strings.Repeat("0", 70_000) + "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := tagBumpRepository(t, tc.tags)
			for _, order := range []string{"refname", "-refname", "version:refname"} {
				cmd := exec.Command("git", "config", "tag.sort", order)
				cmd.Dir = repo
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("configure tag order: %v\n%s", err, output)
				}
				got, ok, err := discoverTagBump(repo, tc.current)
				if err != nil || got != tc.want || ok != (tc.want != "") {
					t.Fatalf("order %s: discoverTagBump = %q, %v, %v; want %q", order, got, ok, err, tc.want)
				}
			}
		})
	}
}

func TestDiscoverTagBumpErrors(t *testing.T) {
	t.Run("current_overflow", func(t *testing.T) {
		_, ok, err := discoverTagBump(t.TempDir(), "v"+strings.Repeat("9", 40))
		if ok || !errors.Is(err, strconv.ErrRange) {
			t.Fatalf("current overflow = %v, %v; want ErrRange before invoking Git", ok, err)
		}
	})
	t.Run("tag_overflow", func(t *testing.T) {
		repo := tagBumpRepository(t, []string{"v1", "v2", "v" + strings.Repeat("9", 40)})
		got, ok, err := discoverTagBump(repo, "v1")
		if got != "" || ok || !errors.Is(err, strconv.ErrRange) {
			t.Fatalf("tag overflow = %q, %v, %v; want no partial bump and ErrRange", got, ok, err)
		}
	})
	t.Run("git_failure", func(t *testing.T) {
		got, ok, err := discoverTagBump(t.TempDir(), "v1")
		var exit *exec.ExitError
		if got != "" || ok || !errors.As(err, &exit) || exit.ExitCode() != 128 {
			t.Fatalf("Git failure = %q, %v, %v; want no partial bump and exit 128", got, ok, err)
		}
	})
}

func TestSelectTagBumpStreams(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   string
	}{
		{output: "", want: ""},
		{output: "v1.2\nv1.10\nv1.03\n", want: "v1.10"},
		{output: "\n \tv1.2\nv1.9\u00a0\n", want: "v1.9"},
		{output: "v1.9\u00a0\nv1.2\n", want: "v1.2"},
		{output: "v1.2\nv1.9\u00a0\n \t\n", want: "v1.9"},
		{output: "v1.2\nv1.9\r\nv1.3\n", want: "v1.3"},
		{output: "v1.2\nv1.9", want: "v1.9"},
		{output: "v1.\xff\nv1.2\n", want: "v1.2"},
	} {
		for _, oneByte := range []bool{false, true} {
			var r io.Reader = strings.NewReader(tc.output)
			if oneByte {
				r = iotest.OneByteReader(r)
			}
			got, err := selectTagBump(r, "v1.", 0)
			if err != nil || got != tc.want {
				t.Fatalf("%q, fragmented=%v: got %q, %v; want %q", tc.output, oneByte, got, err, tc.want)
			}
		}
	}
	t.Run("read_failure_discards_candidate", func(t *testing.T) {
		refusal := errors.New("tag stream failed")
		r := io.MultiReader(strings.NewReader("v1.2\nv1.3\n"), iotest.ErrReader(refusal))
		got, err := selectTagBump(r, "v1.", 0)
		if got != "" || !errors.Is(err, refusal) {
			t.Fatalf("read failure = %q, %v; want no candidate and original error", got, err)
		}
	})
}

func TestDiscoverTagBumpDrainsOutputBeforeWait(t *testing.T) {
	if root := os.Getenv("AM_TEST_TAG_STREAM_ROOT"); root != "" {
		t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
		got, ok, err := discoverTagBump(root, "v1")
		if got != "" || ok {
			t.Fatalf("partial bump escaped failure: %q, %v", got, ok)
		}
		if os.Getenv("AM_TEST_TAG_STREAM_EXIT") == "0" {
			if !errors.Is(err, strconv.ErrRange) {
				t.Fatalf("successful Git must retain tag overflow: %v", err)
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("Git exit must take precedence over tag overflow: %v", err)
			}
		}
		return
	}
	root := t.TempDir()
	script := "#!/bin/sh\nprintf 'v2\\nv" + strings.Repeat("9", 40) + "\\n'\ncat \"$AM_TEST_TAG_STREAM_ROOT/tail\" || exit 1\nexit \"$AM_TEST_TAG_STREAM_EXIT\"\n"
	if err := os.WriteFile(filepath.Join(root, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// More than a pipe buffer must still be consumed after the parse refusal.
	if err := os.WriteFile(filepath.Join(root, "tail"), []byte(strings.Repeat("vignored\n", 128*1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, exit := range []string{"0", "23"} {
		t.Run("exit_"+exit, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestDiscoverTagBumpDrainsOutputBeforeWait$")
			cmd.Env = append(os.Environ(), "AM_TEST_TAG_STREAM_ROOT="+root, "AM_TEST_TAG_STREAM_EXIT="+exit)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bounded drain witness: %v (context: %v)\n%s", err, ctx.Err(), output)
			}
		})
	}
}

func FuzzSelectTagBumpMatchesBufferedSelection(f *testing.F) {
	for _, seed := range []string{"", "v1.2\nv1.10\n", "v1.9\u00a0\nv1.2\n", "v1.2\nv1.9\u00a0\n \t\n", "v1." + strings.Repeat("9", 40)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		if len(output) > 128*1024 {
			t.Skip("bounded stream fixture")
		}
		want, wantErr := bufferedTagBumpOracle(output)
		got, gotErr := selectTagBump(strings.NewReader(output), "v1.", 0)
		if got != want || (wantErr == nil) != (gotErr == nil) || (wantErr != nil && !errors.Is(gotErr, strconv.ErrRange)) {
			t.Fatalf("streamed = %q, %v; buffered = %q, %v", got, gotErr, want, wantErr)
		}
		if wantErr != nil {
			var wantNum, gotNum *strconv.NumError
			if !errors.As(wantErr, &wantNum) || !errors.As(gotErr, &gotNum) || gotNum.Num != wantNum.Num || gotNum.Func != wantNum.Func {
				t.Fatalf("overflow must retain the first failing suffix: got %v, want %v", gotErr, wantErr)
			}
		}
	})
}

// Freeze the original buffered selection, including its whole-output trimming.
func bufferedTagBumpOracle(output string) (string, error) {
	best, number := "", 0
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", nil
	}
	for _, tag := range strings.Split(trimmed, "\n") {
		suffix := strings.TrimPrefix(tag, "v1.")
		if suffix == "" || strings.ContainsFunc(suffix, func(r rune) bool { return r < '0' || r > '9' }) {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil {
			return "", err
		}
		if n > number || (n == number && tag > best) {
			best, number = tag, n
		}
	}
	return best, nil
}

// Packed refs keep fixture setup proportional without one subprocess per tag.
func tagBumpRepository(t testing.TB, tags []string) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "--object-format=sha1", repo)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize tag repository: %v\n%s", err, output)
	}
	var refs strings.Builder
	refs.WriteString("# pack-refs with: peeled fully-peeled\n")
	for _, tag := range tags {
		fmt.Fprintf(&refs, "%s refs/tags/%s\n", strings.Repeat("a", 40), tag)
	}
	if err := os.WriteFile(filepath.Join(repo, "packed-refs"), []byte(refs.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}
