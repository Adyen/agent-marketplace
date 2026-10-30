## ADDED Requirements

### Requirement: Subagent tool strings are read as lists

Claude documents a subagent's `tools` and `disallowedTools` as either a YAML list or a comma-separated string such as `Read, Grep, Bash`. Claude and Cursor source layout ingestion MUST read such a string as the list of its comma-separated entries: entries MUST keep their order, MUST be trimmed of surrounding whitespace, and blank entries MUST be dropped. A comma inside parentheses, as in `Agent(worker, researcher)`, MUST belong to its entry and MUST NOT split it, and a backslash MUST make the character after it literal. A string that names no tool, whose parentheses do not balance, or that ends in a backslash with nothing after it is not a well-formed list and MUST be kept as a single entry exactly as written, unchanged from earlier versions, so that every subagent that loaded before still loads and a source revision that lists it still verifies. A YAML list MUST keep each entry as written.

#### Scenario: Comma-separated subagent tool strings become separate tools

- **WHEN** a Claude source layout subagent declares `tools: Read, Grep` and is converted to Claude, Factory and Gemini, directly and through `sync`
- **THEN** Claude MUST emit `Read` and `Grep` as separate tools, and Factory and Gemini MUST emit the subagent with the tools the YAML list `Read`, `Grep` gives, without `subagent.factory.tool-unknown` or `subagent.gemini.tool-unknown`
- **AND** a subagent declaring `disallowedTools: Bash, Write` MUST be emitted on Claude with `Bash` and `Write` as separate disallowed tools

#### Scenario: Cursor source subagent tool strings become separate tools

- **WHEN** a Cursor source layout subagent declares `tools: Read, Grep` and is converted to Claude
- **THEN** Claude MUST emit `Read` and `Grep` as separate tools and MUST NOT report `subagent.claude.tool-unknown`

#### Scenario: Parentheses keep a scoped subagent tool in one piece

- **WHEN** a Claude source layout subagent declares `tools: Agent(worker, researcher), Read` as a string, and another declares the same two entries as a YAML list, and both are converted to Claude
- **THEN** each MUST be emitted with exactly two tools, `Agent(worker, researcher)` and `Read`

#### Scenario: A backslash makes the next character literal

- **WHEN** a Claude source layout subagent declares `tools: Bash(echo \)), Read`, another declares `tools: Read, Bash(a\), b)`, and another declares `tools: Read\, Grep`, and all are converted to Claude
- **THEN** the first MUST be emitted with two tools, `Bash(echo \))` and `Read`, the second with two tools, `Read` and `Bash(a\), b)`, and the third with the single tool `Read\, Grep`

#### Scenario: A subagent tool string that is not well formed is kept as a single entry

- **WHEN** a Claude source layout subagent declares `tools: ""`, `tools: " , "`, `tools: Read, Bash(git diff`, `tools: Read), Grep`, `tools: Read, Grep\` or `disallowedTools: Bash(rm`, next to a valid subagent, and the source is converted to Claude
- **THEN** conversion MUST emit the subagent with one entry equal to the string as written, MUST report no ingestion error, and MUST emit the valid subagent
- **AND** conversion of the `tools` cases to Factory and Gemini MUST keep reporting `subagent.factory.tool-unknown` and `subagent.gemini.tool-unknown`, as earlier versions did
- **AND** a Devin source layout subagent with `allowed-tools: [read]` and `tools: Read, Bash(git diff` MUST still be ingested, and `tools: ""` converted from Claude to Cursor MUST still report `subagent.cursor.tool-unknown`
- **AND** `sync` MUST install such a subagent next to a valid one, and `sync --check` MUST succeed afterwards
