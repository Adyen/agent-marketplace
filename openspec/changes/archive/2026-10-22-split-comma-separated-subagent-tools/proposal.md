## Why

Claude Code documents a subagent's `tools` as a comma-separated string such as `Read, Grep, Bash` or a YAML list, but ingestion kept the string as one tool named `Read, Grep` (#822). Claude re-emitted the subagent with a single combined entry, and Factory and Gemini suppressed it with `subagent.factory.tool-unknown` and `subagent.gemini.tool-unknown`. A subagent written in Claude's own documented form therefore lost its tool restriction on Claude and was dropped everywhere else. `disallowedTools` shared the defect, so a combined `Bash, Write` entry denied nothing.

## What Changes

- Claude and Cursor source layout subagent ingestion reads a `tools` or `disallowedTools` string as the list of its comma-separated entries, in order, trimmed, with blank entries dropped. A comma inside parentheses belongs to its entry, so `Agent(worker, researcher)` stays whole, and a backslash makes the next character literal. A YAML list is read as written.
- A string that names no tool, whose parentheses do not balance, or that ends in a backslash with nothing after it is not a well-formed list. It keeps the single entry earlier versions produced, exactly as written. #822 asks for such a value to be refused, and the design explains why it is not.

## Capabilities

### Modified Capabilities

- `harness-compilation`: subagent tool strings are read as lists.

## Impact

`bundle.LoadSubagent`, which Claude and Cursor source layout ingestion, Devin ingestion for the key it ignores, and the canonical `LoadSubagent` loader share. A subagent that spells `tools` or `disallowedTools` as a well-formed string with commas now carries one entry per tool. Claude emits a list with separate entries, a Cursor source converts to Claude instead of being suppressed, Factory and Gemini emit the subagent instead of suppressing it, and `disallowedTools` now denies each named tool. A single name is unchanged except that surrounding whitespace is trimmed, so a padded quoted name such as `" Read "` becomes a known tool where Factory and Gemini used to suppress it as unknown. Every string that is not well formed is unchanged.

For these subagents the Marketplace capability entries (`tool:<name>`) and tool-grant finding texts now list each tool, and the content and archive digests of artifacts built from a re-ingested revision change, while payloads published earlier stay as they were until the revision is published again. Source-revision digests hash raw source bytes and the inventory is unchanged, so locks written by earlier releases keep verifying. Skill `allowed-tools` strings and the Factory, Gemini and Devin readers of their own tool lists are not touched.
