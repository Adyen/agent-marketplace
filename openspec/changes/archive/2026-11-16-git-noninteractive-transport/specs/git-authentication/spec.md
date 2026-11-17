## ADDED Requirements

### Requirement: Git transport never prompts on the terminal

Every Git subprocess that Agent Marketplace starts, from the CLI or the TUI, SHALL run without access to the controlling terminal on Unix platforms and with `SSH_ASKPASS_REQUIRE=never`. A transport program such as `ssh` MUST NOT be able to read from or write to the terminal, so host-key confirmations and key passphrase prompts cannot appear. Agent Marketplace MUST NOT accept an unknown SSH host key on the user's behalf.

#### Scenario: SSH prompt cannot reach the TUI

- **WHEN** the TUI install-from-source flow previews a Git source whose SSH transport tries to prompt on `/dev/tty`
- **THEN** no prompt text MUST appear on the screen, the TUI MUST keep receiving key presses, and the flow MUST show the transport failure or the HTTPS fallback result

#### Scenario: CLI acquisition does not prompt

- **WHEN** a CLI command acquires a Git source whose SSH transport tries to open `/dev/tty`
- **THEN** opening the terminal MUST fail, the command MUST NOT wait for input, and it MUST fail with `git.auth-transport-failed`

#### Scenario: Unknown host keys are not trusted automatically

- **WHEN** SSH acquisition reaches a host whose key is not in the user's known hosts
- **THEN** acquisition MUST fail and the user's known hosts files MUST be unchanged

### Requirement: Transport failures name the likely cause

A `git.auth-transport-failed` diagnostic SHALL name the likely cause when Git's error output matches a known class: an unverified SSH host key, or an HTTP(S) repository that is missing or requires credentials (failed authentication, or HTTP 401, 403 or 404). The diagnostic MUST NOT include Git's output. Unmatched failures keep the existing diagnostics.

#### Scenario: Unverified SSH host key is explained

- **WHEN** SSH acquisition of `git@github.com:owner/repo.git` fails with host key verification failure
- **THEN** the diagnostic MUST be `git.auth-transport-failed`, MUST name `github.com`, and MUST tell the user to verify the host key and add it to `~/.ssh/known_hosts`

#### Scenario: Missing or private HTTPS repository is explained

- **WHEN** HTTPS acquisition fails because Git reports failed authentication or a missing repository
- **THEN** the diagnostic MUST be `git.auth-transport-failed` saying the repository was not found or requires credentials, and MUST NOT contain Git's output, the URL's userinfo, or any credential

#### Scenario: Web URL SSH host-key failure still falls back to HTTPS

- **WHEN** a GitHub or GitLab web URL's SSH probe fails host key verification and its HTTPS form is reachable
- **THEN** the source MUST use the HTTPS form with the existing fallback warning and no terminal prompt

#### Scenario: Unclassified transport failures keep the existing diagnostic

- **WHEN** a transport failure's Git output matches no known class
- **THEN** the diagnostic MUST be the existing SSH or withheld HTTP(S) `git.auth-transport-failed` message
