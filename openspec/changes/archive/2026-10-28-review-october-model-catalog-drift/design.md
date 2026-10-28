## Approach

Keep model resolution unchanged and finish the proposal as reviewed catalog
data. Codex's model inventory and subagent documentation establish the model
field; GPT-6.1 Terra's model page establishes its reasoning levels. Ship the
documented Codex levels low, medium, high, xhigh, and max without inferring
API-only none support for custom agents.

Factory's model inventory establishes the six proposed native IDs. Intersect
each model's reasoning levels with the custom-droid field's documented
low/medium/high values. Do not infer custom-droid support for session-only
none, off, xhigh, or max.

The existing Devin catalog already maps GPT-6 priority variants to distinct
fast identities. Factory's new matching identity makes conversion resolvable,
so update the old unavailable-destination fixture and retain a negative
control targeting Codex, which has no matching fast entry.

## Evidence Review

Reviewed on 2026-10-28:

- https://developers.openai.com/codex/models.md: GPT-6.1 Terra is listed; it
  requires Codex CLI 0.159.0 or later.
- https://developers.openai.com/codex/subagents.md: custom agents accept model
  and model_reasoning_effort.
- https://developers.openai.com/api/docs/models/gpt-6.1-terra.md: the model
  documents low, medium, high, xhigh, and max, plus API none.
- https://docs.factory.ai/models.md: all six proposed IDs and their individual
  reasoning levels are listed.
- https://docs.factory.ai/harness/subagents.md: custom droids accept public
  model IDs and compatible low/medium/high reasoningEffort values.

Refresh the two reviewed inventory hashes and add a baseline for the new
Terra page. Leave other source hashes unchanged: adding mappings is not
acknowledgement of unrelated model, hook, or tool documentation.

## Verification

Use the existing conversion-fixture runner and public compiler table rather
than adding a test framework. Cover every new identity, priority conversion in
both directions, an unavailable destination, and unsupported reasoning.
Verify fixtures fail against the previous catalog data before accepting them.
