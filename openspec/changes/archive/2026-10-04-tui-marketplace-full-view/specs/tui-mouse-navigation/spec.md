## ADDED Requirements

### Requirement: Full-view text selection follows the displayed preview

Full-view bundle labels SHALL remain within the tree's allocated columns at supported terminal sizes. Unmodified left-button drags in the right-hand preview SHALL highlight and copy exactly the displayed Unicode text through the terminal clipboard protocol, independent of label length.

#### Scenario: Long bundle labels do not displace selectable preview text

- **WHEN** a user opens full view with a bundle label longer than the tree width at a supported narrow terminal size and drags across displayed Unicode text in the right-hand preview
- **THEN** the tree MUST stay within its allocated columns and the drag MUST highlight and copy exactly that displayed preview text without including tree text or padding

### Requirement: Marketplace full-view scrolling preserves selection boundaries

Marketplace full-view browsing SHALL retain pointer-targeted wheel routing, selection cancellation on scrolling, resize and content replacement, and the existing refusal to begin clipboard selection in the tree, footer, or overlay.

#### Scenario: Marketplace selection and scrolling preserve clipboard boundaries

- **WHEN** a user selects Marketplace preview text, scrolls the tree or preview, resizes, changes the preview, or starts a drag in the tree, footer, or overlay
- **THEN** existing wheel routing MUST remain intact, stale selection MUST clear, and gestures starting outside the selectable preview MUST NOT write to the clipboard
