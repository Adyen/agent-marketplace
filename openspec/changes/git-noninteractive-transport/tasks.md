## 1. Implementation

- [x] 1.1 Detach Git subprocesses (only those; the editor and other processes keep the terminal) from the controlling terminal on Unix (new session, no inherited terminal stdin) and set `SSH_ASKPASS_REQUIRE=never` in `gitEnv`, with tests proving a fake `GIT_SSH` cannot open `/dev/tty`. Capture acquisition stderr for classification instead of discarding it.
- [x] 1.2 Classify captured Git stderr into host-key and not-found/credentials causes without exposing it, in `gitTransportFailureDiagnostic`, with unit tests for each class, userinfo URLs, and the unclassified fallback
- [x] 1.3 Update the `git.auth-transport-failed` remedy hint and the private-registry guide

## 2. TUI QA

- [x] 2.1 Add or extend a TUI QA journey: install-from-source with a fake `GIT_SSH` that prompts on `/dev/tty`; assert no prompt text appears, the diagnostic shows, and Esc still closes the modal. The journey must fail without 1.1. List it in `tools/tui-qa/README.md`.

## 3. Archive

- [ ] 3.1 Run `openspec archive git-noninteractive-transport --yes` as the final commit
