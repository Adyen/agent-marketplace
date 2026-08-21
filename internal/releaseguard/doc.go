// Package releaseguard holds the regression tests for the release tag
// admission contract.
//
// A pushed Git tag name is untrusted input that reaches privileged automation:
// .github/workflows/ci.yml derives VERSION from it, the Makefile stamps it into
// binaries and artifact filenames, and Git permits tag names containing shell
// and Make syntax. The enforcement lives outside Go — in
// scripts/validate-release-tag.sh, the Makefile, and the workflow — so this
// package deliberately contains no production code: it exercises those artifacts
// as a subprocess and asserts their structure, keeping the guarantees from
// silently regressing when release automation is edited.
package releaseguard
