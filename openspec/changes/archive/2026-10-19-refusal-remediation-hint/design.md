## Context

Version refusals are produced while loading a consumer root or include tree, where the loader already knows the owning layer. Resource-level errors expose remediation through an existing remediation interface that the TUI diagnostics view renders ahead of category copy; command errors are routed through one terminal-safety wrapper before Cobra prints them.

## Goals / Non-Goals

**Goals:** attach layer-aware guidance to the two version refusal codes at the point the layer is known, and render it through the existing remediation surfaces.

**Non-Goals:** a hosted docs URL, guidance for other `v4.*` codes, or any change to refusal timing, exit codes, or the TUI startup stages.

## Decisions

- **Guidance lives on the validation error.** The version refusal error carries its owning layer and exposes remediation only for the two version codes. The TUI picks it up through its existing remediation lookup; no TUI-specific copy is added. Alternative: map codes to copy inside the TUI and CLI separately, rejected because it duplicates text and loses the layer.
- **CLI appends guidance in the command error wrapper, narrowly.** Only version refusals gain the appended line, so other command errors keep their output. Commands that already print remediation for listed errors must not show it twice.
- **Link to the GitHub-hosted guide source.** The docs hosting target is undecided; the reference docs already link to GitHub source for the diagnostic catalog.
- **Fragments use the owning root's layer.** The include tree inherits the layer from its root, so an organization fragment gets the administrator variant.

## Risks / Trade-offs

- [The pinned v4.1.4 version ages] → it is the last release shipping `migrate --to v4`; later v4-only releases never regain it, so the pin stays correct.
- [Versionless legacy roots may need manual structural work before the migrator accepts them] → the guidance says conversion can refuse and links the guide that covers this.
