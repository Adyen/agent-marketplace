## ADDED Requirements

### Requirement: Landing page offers desktop GUI downloads

The public project website's landing page SHALL offer desktop GUI downloads next to the unchanged CLI install command. It SHALL build download links from the asset names published by the latest stable GitHub release, offering the macOS `.dmg` as the primary action to macOS visitors and the `.rpm` and `.deb` for each Linux architecture to Linux visitors, and all of them to other visitors. When the release lookup fails or the latest stable release publishes no GUI assets, it SHALL link to the latest-release page and SHALL NOT link to a GUI asset URL. While the macOS app is ad-hoc signed, the landing page SHALL explain how to open it the first time.

#### Scenario: macOS visitor sees the disk image download

- **WHEN** a macOS visitor opens the landing page and the latest stable release publishes GUI assets
- **THEN** the primary GUI download SHALL link to that release's macOS `.dmg`
- **AND** the CLI install command SHALL remain shown unchanged
- **AND** the page SHALL explain how to open the app the first time

#### Scenario: Linux visitor sees package downloads

- **WHEN** a Linux visitor opens the landing page and the latest stable release publishes GUI assets
- **THEN** the page SHALL link to that release's `.rpm` and `.deb` for `amd64` and `arm64`, labeled by format and architecture

#### Scenario: Release lookup failure falls back to the release page

- **WHEN** the latest-release lookup fails or the latest stable release publishes no GUI assets
- **THEN** the GUI download SHALL link to the latest-release page
- **AND** the page SHALL NOT link to a GUI asset URL
