# GUI desktop release notes

## Desktop downloads and in-app updates

The desktop GUI ships as a universal macOS disk image and zip, and as `.rpm` and `.deb` packages for Linux on amd64 and arm64. When a newer stable release is published, the GUI shows a notice. On macOS, clicking Update downloads, verifies, and replaces the app in place; on Linux, the GUI downloads and verifies the package and opens it in the software installer. Nothing is downloaded until the user clicks.

Release checks follow the shared `update_check.enabled` setting used by the CLI and TUI. `AGENT_MARKETPLACE_NO_UPDATE_CHECK` and `CI` also turn them off. The earlier `gui.version_check` key was never released and is rejected as an unknown field.

See [Desktop app](../site/docs/install.md#desktop-app) and the [configuration reference](../site/docs/reference/configuration.md).
