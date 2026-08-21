package cache

import (
	"os"
	"path/filepath"
	"strings"
)

// gitScopingEnvNames are the environment variables that tell git *which*
// repository, index, object store or config to operate on.
//
// They have to be removed rather than merely overridden, because they defeat the
// containment this package is built on from outside it: the cache decides which
// directory git works in by handing it a validated descriptor, and any one of
// these variables re-points git at a repository, an index or an object store
// somewhere else entirely — so a `checkout --force` meant for a cache entry lands
// in whatever repository the ambient environment named. An agent-marketplace run
// inherits its environment from a shell, a hook, a CI job or a parent git process
// (git sets GIT_DIR and GIT_INDEX_FILE for every hook it runs), so this is the
// ordinary case, not an exotic one.
//
// Transport and authentication variables (GIT_SSH_COMMAND, GIT_ASKPASS,
// credential helpers, proxy settings) are deliberately *not* removed: they are
// how a user reaches a private registry, and they cannot change which repository
// is written to.
var gitScopingEnvNames = map[string]struct{}{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
	"GIT_CEILING_DIRECTORIES":          {},
	"GIT_COMMON_DIR":                   {},
	"GIT_CONFIG":                       {},
	"GIT_CONFIG_COUNT":                 {},
	"GIT_CONFIG_GLOBAL":                {},
	"GIT_CONFIG_PARAMETERS":            {},
	"GIT_CONFIG_SYSTEM":                {},
	"GIT_DIR":                          {},
	"GIT_DISCOVERY_ACROSS_FILESYSTEM":  {},
	"GIT_GRAFT_FILE":                   {},
	"GIT_INDEX_FILE":                   {},
	"GIT_INDEX_VERSION":                {},
	"GIT_NAMESPACE":                    {},
	"GIT_OBJECT_DIRECTORY":             {},
	"GIT_PREFIX":                       {},
	"GIT_REPLACE_REF_BASE":             {},
	"GIT_SHALLOW_FILE":                 {},
	"GIT_WORK_TREE":                    {},
}

// gitScopingEnvPrefixes cover the numbered halves of GIT_CONFIG_COUNT. They are
// inert without the count, but leaving them in place would make a future
// GIT_CONFIG_COUNT we set ourselves pick up values we did not choose.
var gitScopingEnvPrefixes = []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"}

func isGitScopingEnvName(name string) bool {
	if _, found := gitScopingEnvNames[name]; found {
		return true
	}
	for _, prefix := range gitScopingEnvPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// gitEnviron returns the environment for a git subprocess anchored to dir: the
// process environment with every repository-scoping variable removed, then the
// scoping this package wants added back.
//
// The scoping added back stops *upward* repository discovery. Git's default
// behaviour when a directory is not a repository is to walk towards the root
// looking for one, so a cache entry that is missing its `.git` — an interrupted
// clone, a half-finished `rm` — would otherwise make git operate on whichever
// repository happens to contain the cache. That is not hypothetical: the cache
// lives under the home directory, and `CacheDir` falls back to a path under the
// working directory when there is no home directory to use.
func gitEnviron(dir *os.File, extra ...string) []string {
	environ := os.Environ()
	sanitized := make([]string, 0, len(environ)+len(extra)+2)
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || isGitScopingEnvName(name) {
			continue
		}
		sanitized = append(sanitized, entry)
	}

	// A ceiling entry must be absolute to have any effect, and the list is
	// colon-separated, so a path containing a colon cannot be expressed. Both
	// cases only lose this defence in depth; the explicit --git-dir passed to
	// every repository operation is what actually prevents discovery.
	if ceiling := filepath.Clean(dir.Name()); filepath.IsAbs(ceiling) && !strings.Contains(ceiling, ":") {
		sanitized = append(sanitized, "GIT_CEILING_DIRECTORIES="+ceiling)
	}
	sanitized = append(sanitized, "GIT_DISCOVERY_ACROSS_FILESYSTEM=false")

	return append(sanitized, extra...)
}
