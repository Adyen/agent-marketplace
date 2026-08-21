//go:build unix

package cache

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// gitAnchorEnv marks a re-exec of this binary whose only job is to become git
// with its working directory taken from an inherited descriptor. gitAnchorFD is
// the descriptor number, which is fixed because exec.Cmd.ExtraFiles always
// starts at 3.
const (
	gitAnchorEnv   = "AGENT_MARKETPLACE_GIT_ANCHOR"
	gitAnchorValue = "fchdir"
	gitAnchorFD    = 3
)

// gitExecutionIsAnchored reports whether git subprocesses take their working
// directory from a descriptor rather than from a path.
const gitExecutionIsAnchored = true

func init() { becomeAnchoredGit() }

// becomeAnchoredGit replaces this process with git, chdir'ed by descriptor.
//
// Why a re-exec at all: os/exec can only express a child's working directory as
// a *string*, and the kernel re-resolves that string when the child chdirs.
// Every containment check this package makes happens before that resolution, so
// handing git a path leaves a window in which swapping one directory component
// for a symlink redirects everything git writes to a location outside the cache
// — precisely the escape the anchoring exists to prevent. fchdir(2) on a
// descriptor this process opened and validated has no such window: a descriptor
// names an inode, and an inode cannot be swapped. Every path git is then given
// is relative to that descriptor, so git never resolves a cache path at all.
//
// Why init() rather than a hidden subcommand: the re-exec target is whichever
// binary linked this package — the release binary in production, a `go test`
// binary under test, and any other command that reaches CloneOrPull — so no
// single main() can be relied on to install the hook.
func becomeAnchoredGit() {
	if os.Getenv(gitAnchorEnv) != gitAnchorValue {
		return
	}
	// A stray environment variable must not hijack an ordinary invocation. A real
	// anchor always inherits a directory on gitAnchorFD; anything else is not one
	// of ours, so leave the process alone.
	var info syscall.Stat_t
	if err := syscall.Fstat(gitAnchorFD, &info); err != nil || info.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return
	}

	if err := syscall.Fchdir(gitAnchorFD); err != nil {
		anchorFatal("fchdir anchor descriptor: %v", err)
	}
	// Closing the anchor leaves git with exactly the descriptor table a plain
	// exec would have given it, so nothing downstream can observe or reuse it.
	_ = syscall.Close(gitAnchorFD)

	gitPath, err := exec.LookPath("git")
	if err != nil {
		anchorFatal("locate git: %v", err)
	}

	environ := os.Environ()
	forwarded := make([]string, 0, len(environ))
	for _, entry := range environ {
		if strings.HasPrefix(entry, gitAnchorEnv+"=") {
			continue
		}
		forwarded = append(forwarded, entry)
	}

	if err := syscall.Exec(gitPath, append([]string{"git"}, os.Args[1:]...), forwarded); err != nil {
		anchorFatal("exec git: %v", err)
	}
}

func anchorFatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-marketplace: anchored git: "+format+"\n", args...)
	// 127 is the conventional "command could not be executed" status, and it is
	// not a status git itself produces for a failed operation.
	os.Exit(127)
}

// gitCommand builds a git invocation whose working directory is dir, taken by
// descriptor. args must only contain names relative to dir.
func gitCommand(dir *os.File, args []string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate own executable to anchor git: %w", err)
	}
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), gitAnchorEnv+"="+gitAnchorValue)
	cmd.ExtraFiles = []*os.File{dir}
	return cmd, nil
}
