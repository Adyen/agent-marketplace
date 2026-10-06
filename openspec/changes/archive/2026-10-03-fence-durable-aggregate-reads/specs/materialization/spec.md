## ADDED Requirements

### Requirement: Durable native aggregate readers stay bound to canonical target roots

Agent Marketplace durable native aggregate reconciliation and repair MUST use the ownership store's scoped captured reader rather than reopening a lexical aggregate pathname. A target-root retarget after store/session admission MUST NOT import a foreign aggregate as a healthy no-op. Valid native installation, unchanged repeats, root aliases admitted canonically, private creation, handwritten settings and durable ownership semantics MUST remain supported. These reader controls do not close publication or journal/state consumers.

#### Scenario: Durable aggregate reader retains canonical root

- **WHEN** native Gemini aggregate repeat encounters a target-root retarget after an actual installed-generation and ownership-session acquisition
- **THEN** materialization MUST report refusal instead of foreign no-op success and preserve original and foreign aggregate bytes
