## ADDED Requirements

### Requirement: Reviewed Codex and Factory model additions

Subagent conversion SHALL resolve `openai/gpt-6.1-terra` to Codex's native
`gpt-6.1-terra` model with low, medium, high, xhigh, and max reasoning.
Factory SHALL resolve `deepseek/deepseek-v4.1-flash-1017`,
`minimax/minimax-m3.1-flash`, `nvidia/nemotron-3.5-super`,
`openai/gpt-6-luna-fast`, `openai/gpt-6-sol-fast`, and
`openai/gpt-6.1-sol-ultrafast` to their corresponding unqualified native model
IDs. Factory reasoning support SHALL remain limited to the intersection of
each model's documented levels and the custom-droid field's low/medium/high
levels. Unsupported reasoning SHALL retain the selected model and report
`model.reasoning-omitted`.

#### Scenario: Reviewed additions emit native model and reasoning fields

- **WHEN** a subagent selects one of the reviewed model additions for its
  supported destination with a supported reasoning level
- **THEN** conversion SHALL emit that model's native ID and reasoning value

#### Scenario: Priority variants retain their distinct fast identity

- **WHEN** a Devin GPT-6 priority variant is converted to Factory or a matching
  Factory fast model is converted to Devin
- **THEN** conversion SHALL retain the corresponding fast identity and
  supported reasoning level
- **AND** a destination without that fast identity SHALL report
  `model.unresolved` rather than substitute the base model

#### Scenario: Reviewed additions omit unsupported reasoning without substitution

- **WHEN** a reviewed model addition is selected with an unsupported reasoning
  level for the destination resource format
- **THEN** conversion SHALL retain the model and omit the reasoning value
- **AND** it SHALL report `model.reasoning-omitted` without selecting a
  different effort level
