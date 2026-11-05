## What changed and why

<!-- Describe the change and the problem it solves. -->

Fixes #<!-- issue number, or remove this line -->

## Type of change

- [ ] Bug fix
- [ ] New feature or behavior change
- [ ] Support for a harness (`claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, `opencode`, `windsurf`)
- [ ] Documentation only
- [ ] Build, CI, or tooling

## Harnesses affected

<!-- List the harnesses whose output or behavior changes, or write "none". -->

## How you tested it

<!-- Commands you ran and what you saw. -->

- [ ] `make validate` passes
- [ ] `make spec-check` passes (needed when behavior or specs change)
- [ ] `make validate-e2e` passes (needed when sync, install, or harness output changes)
- [ ] `make tui-qa` passes (needed when the terminal UI changes)

## Specs and documentation

- [ ] This change updates `openspec/` with a spec delta, or the commit carries a `Spec-Impact: none - <reason>` trailer because behavior is unchanged
- [ ] User-facing changes are reflected in `site/docs`
- [ ] Each new `openspec/changes/*/proposal.md` has a `## Release note` (user-facing sentences, `Breaking: ...` with upgrade steps, or `none - <reason>`)

## Before you submit

- [ ] The change contains no secrets, tokens, internal hostnames, or machine-specific paths
- [ ] I'm contributing this under the Apache 2.0 license, as described in `LICENSE`
