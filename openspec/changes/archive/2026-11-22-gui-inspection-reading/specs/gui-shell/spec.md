## ADDED Requirements

### Requirement: Formatted Markdown in resource inspection

Resource inspection SHALL show a selected Markdown file formatted by default, with leading frontmatter shown verbatim above the formatted body, and SHALL offer a Source view of the unmodified file. Formatting SHALL NOT execute or render raw HTML, SHALL NOT load images, and SHALL NOT navigate the application window; only http and https links SHALL be activatable, and they SHALL open in the system browser.

#### Scenario: Markdown file opens formatted with a Source view

- **WHEN** the user previews a Markdown file with frontmatter, headings, emphasis, and code
- **THEN** the body MUST be shown formatted with the frontmatter verbatim above it
- **AND** choosing Source MUST show the unmodified file text

#### Scenario: Untrusted Markdown stays inert

- **WHEN** a previewed Markdown file contains raw HTML, an image, and links with http, javascript, and file schemes
- **THEN** the raw HTML MUST appear as text, no image MUST be requested, only the http link MUST be activatable, and activating it MUST open the system browser without navigating the application window

### Requirement: Dialogs clear the macOS window controls

On macOS, every GUI dialog SHALL be placed below the window's close, minimize, and zoom controls, and resource inspection SHALL offer a single close control in its header.

#### Scenario: Resource inspection clears the window controls

- **WHEN** resource inspection opens on macOS
- **THEN** the dialog's top edge MUST be at least 52 px below the top of the window
- **AND** the dialog MUST NOT show a footer close button
