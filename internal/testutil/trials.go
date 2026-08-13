package testutil

// This file owns the trial budget of the repository's probabilistic race tests
// (bead agent-marketplace-npwr, the test-cost carry-forward item).
//
// The ownership-locking suites repeat a two-writer race many times because a
// race that reproduces 60/60 before a fix must be shown not to reproduce at all
// after it. That is worth its wall clock in a full run and not worth it in the
// inner loop: the 200-trial tests alone add roughly a minute to `make test`, and
// `-race -count=2` across the three affected packages costs about ten minutes.
//
// `go test -short` therefore scales every trial loop down by [ShortDivisor]. The
// reduced run is NOT a weaker version of the same statistical claim, which is the
// only reason scaling is legitimate here: every one of these races reproduced at a
// measured 100% per-trial rate before its fix, so 20 trials is as deterministic as
// 200 and the extra 180 buy repetition rather than confidence. A test that
// genuinely depended on sampling a rare interleaving would have to opt out of this
// helper and say why.
//
// The gate follows the precedent already in the tree — TestNonUnixBuildCompiles
// skips cross-compilation under -short — rather than inventing a second
// convention.

// FullTrials is the default full-run trial count named by the ownership-locking
// beads' acceptance criteria. It is a var so a developer chasing a flake can
// raise it in one place.
var FullTrials = 200

// ShortDivisor is how much `go test -short` divides a trial count by.
var ShortDivisor = 10

// Trials returns how many trials a race test should run.
//
// short is the caller's testing.Short(), passed in rather than read here so that
// this package — which is linked into every test binary that imports it — does
// not import "testing" and register the testing flags in a non-test package.
//
// The result is never below 1: a scaled-down loop that ran zero trials would
// report success without executing the property at all, which is the one outcome
// a cost optimization must not be able to produce.
func Trials(full int, short bool) int {
	if !short {
		return full
	}
	scaled := full / ShortDivisor
	if scaled < 1 {
		return 1
	}
	return scaled
}
