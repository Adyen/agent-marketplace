## Context

The shared Git acquisition preflight copies an allowlist of HTTPS credential and TLS settings into the subprocess environment. Its blanket include refusal rejects ordinary company-managed system/global include chains. The user selected ordinary nested includes for this change; conditional include evaluation remains out of scope.

## Decisions

- Keep Git as the parser, but pass validated file bytes to `git config --file - --no-includes --null --list`. Expand only ordinary `include.path` records ourselves, at their declared position.
- Resolve absolute include paths directly, `~/` against HOME, and relative include paths against the declaring file's authored directory. Keep that directory semantics for symlinked configuration files.
- Open nonblocking, validate the descriptor's regular-file shape, owner (root or effective user), absence of group/other write permission, and single-link count, then read through a size limit. Do not let a later path-based parse replace the validated bytes.
- Limit each file to 4 MiB, the expanded read budget to 16 MiB, expanded file visits to 128, and include depth to Git's ordinary ten-level ceiling. The visit budget bounds native parser subprocess work even for tiny repeated includes. Detect cycles through opened-file identities while allowing the same file on independent include branches. Missing optional roots and include files, and explicit `/dev/null` includes, remain no-ops, as in Git.
- Preserve system-before-global and XDG-before-home ordering, URL-specific keys, and empty helper resets. Copy only the existing authentication allowlist; never apply hooks, URL rewrites, TLS verification disabling, or other Git execution settings from includes.
- Keep conditional includes and repository-local authentication blocked. Keep the stable `git.auth-config-untrusted` identity and sanitized errors, without file contents or authentication values.
- Use the same shared reader for exact and legacy registry acquisition. Test real authenticated clone and subsequent fetch paths, with positive and negative controls and unchanged input configuration bytes.

## Validation

Add failing ordinary-include regression tests before implementation. Cover system/global roots, relative and home paths, nesting, repeated sibling includes, symlinked regular files, helper order and resets, malformed/untrusted/special files, cycles, and bounds. Retain conditional and repository-local rejection coverage. Compare the existing authentication-reader benchmark before and after, and add an included-chain benchmark. Run scoped tests, race checks, the local validation gate, combined E2E/configuration gate, strict OpenSpec validation, and an adversarial review before publication.

### Executed evidence

- The new exact/legacy acquisition and include-order tests failed with the original blanket refusal before implementation, then passed through real authenticated clone and subsequent fetch.
- Focused tests cover caller linked-worktree isolation, TLS verification despite an included `sslVerify=false`, parsing already validated bytes after path replacement, default system-path discovery, the ten-level acceptance boundary, and unsafe-chain rejection before helper or transport execution.
- Targeted race checks pass. The first run under concurrent gates exhausted a five-second test deadline in the deliberately 128-parser visit-budget case; its deadline is now 30 seconds while the FIFO rejection bound stays five seconds.
- Fresh-cache CI lint passes. An initial unused first-acquisition result is now checked against the registry HEAD, strengthening the clone control.
- Five 2-second samples on the same macOS arm64 host measured the no-include reader at 35.09 ms before and 36.40 ms after (`p=0.151`, no significant timing change). Allocation bytes fell 9.26% and allocation count fell 3.69% (`p=0.008`). The two-level included-chain reader, with four native parser subprocesses total, measures 72.16 ms and 248.2 KiB per operation. These include disk and subprocess work, not network or helper execution.
- `make validate TEST_PACKAGES=./internal/cache` passes, including the complete cache-package short-mode suite, fresh-cache CI lint, and build. The initial default aggregate selected all root packages because public documentation paths do not map to a Go package. Its command suite hit the existing 120-second concurrent-load deadline in `TestRemoveBlockedManagedResourcePreservesSiblings`; the prescribed isolated rerun passed all twelve scenarios in 24.82 seconds. That initial aggregate is not counted as a full-suite pass.
- Native parser output expansion has an explicit negative control, passing both normal and race runs, so small input cannot silently bypass the subprocess output budget.
- The combined `make validate-e2e` host E2E/configuration gate and shellcheck passed (865.37 seconds). No native harness probes or historical/released-binary Docker gates were run; this change does not alter harness output or acquisition lock formats.
- The adversarial delegate reviewed all twelve intended staged files against `b647e8f9f4d7357ce9d5222335038a9765c19155`. No critical, high, or medium finding remained. Both low-severity notes were addressed: the local-transport tripwire now exercises and detects system-path discovery, and `/dev/null` includes now match Git's empty-config behavior. The null-include regression failed before the three-line no-op guard, then passed. Final focused tests, race checks, scoped local validation, and spec traceability passed after those fixes.
- The final incremental review checked the exact `/dev/null` sentinel against helper ordering, depth/visit accounting, and transport authority: it contributes no settings, opens no file, and does not bypass budgets or admit other special files. The strengthened discovery fixture remains isolated from host Git configuration.
