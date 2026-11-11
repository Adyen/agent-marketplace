## ADDED Requirements

### Requirement: Landing hero previews switch between Terminal and Web
Landing SHALL show two hero previews selected by a tab list above the product
preview: Terminal and Web. Terminal SHALL keep the existing TUI preview and be
selected on load. Web SHALL show a browser-framed depiction of the real
Marketplace catalog. The caption under the preview SHALL match the selected
tab. The hero tab list SHALL use the shared tab model: tab and tabpanel
relationships, one roving tab stop, and wrapping arrow, Home, and End selection.
The hero arrival animation SHALL be unchanged.

#### Scenario: Hero shows the Terminal preview by default
- **WHEN** a user loads Landing
- **THEN** the Terminal tab is selected and the existing TUI preview is visible
- **AND** the caption reads the Terminal preview caption

#### Scenario: Web tab shows the catalog with a matching caption
- **WHEN** a user selects the Web tab in the hero
- **THEN** a browser-framed image of the Marketplace catalog is visible in the preview
- **AND** the caption changes to the Web preview caption

#### Scenario: Hero tabs wrap keyboard selection
- **WHEN** a user navigates the hero tabs with arrows, Home, or End
- **THEN** focus and selection move together according to the wrapping tab order
- **AND** exactly one hero tab remains selected and keyboard-focusable

#### Scenario: Hero and harness tabs remain independent
- **WHEN** a user changes the hero tab selection and then the harness tab selection
- **THEN** each tab list keeps its own selected tab, roving tab stop, and ID prefix
- **AND** changing one list does not change the other

#### Scenario: Reduced motion switches hero tabs without transition
- **WHEN** a user with reduced motion enabled switches hero tabs
- **THEN** the selected preview and caption are shown immediately
- **AND** no travelling indicator or crossfade animates the change

### Requirement: Landing organizations depict the web UI in a browser frame
The "For organizations" section SHALL replace the terminal-framed Marketplace
mock with a flat browser frame that has a window header, an address bar showing
a neutral host, and no drop shadow. The frame SHALL contain a catalog image and
a resource detail image. The benefit cards and the Deployment guide link SHALL
remain.

#### Scenario: Browser frame shows catalog and resource detail
- **WHEN** a user views the "For organizations" section
- **THEN** a browser frame shows the catalog with mixed resource kinds, the Kind and Registry filters, and the "Copy install command" action
- **AND** the frame shows a resource detail image with a pinned revision digest and a clean Security result

#### Scenario: Browser frame avoids linking a live Marketplace
- **WHEN** the public site is built
- **THEN** the browser frame address bar shows a neutral host and not the Marketplace path
- **AND** no page links to, prefetches, or embeds a local `/marketplace/` route from the depiction

#### Scenario: Benefit cards and deployment guide remain
- **WHEN** a user views the "For organizations" section
- **THEN** the three benefit cards are present with their headings
- **AND** the Deployment guide link is present and targets the deployment guide

### Requirement: Landing web UI depictions follow the page appearance
Every web UI image on Landing SHALL have a light and a dark variant generated
from the real Marketplace app by one capture command. Exactly one variant SHALL
be visible for the page appearance, selected by VitePress's `.dark` class on
`html`, including after the appearance is toggled without reloading. Images
SHALL declare explicit dimensions, meaningful alternative text, and lazy
loading when below the fold. The hero Web depiction SHALL NOT delay the hero's
first render.

#### Scenario: Visible depiction follows the dark class
- **WHEN** a user toggles Landing between light and dark appearance without reloading
- **THEN** the visible image variant changes to match the appearance
- **AND** the hidden variant is not displayed

#### Scenario: Depictions declare dimensions alt text and lazy loading
- **WHEN** Landing HTML is inspected
- **THEN** each web UI image has explicit width and height, non-empty alternative text, and `loading="lazy"` when it is below the fold
- **AND** the hero Web depiction is not loaded before the Web tab is selected

#### Scenario: Regenerated depictions are reproducible
- **WHEN** the capture command runs twice on an unchanged tree
- **THEN** the second run produces no visible difference in the committed images
- **AND** every committed image is named by the capture manifest
