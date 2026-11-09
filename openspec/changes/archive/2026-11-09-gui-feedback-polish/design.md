# Design

## App name

Wails names the macOS application menu from `NSRunningApplication.localizedName`, which reads the main bundle's `CFBundleName`. The packaged `.app` already carries `CFBundleName` "Agent Marketplace"; the unbundled executable has no bundle, so macOS falls back to the process name `agent-marketplace-gui`. The darwin native host embeds a minimal Info.plist (`CFBundleName` and `CFBundleDisplayName` "Agent Marketplace") into the executable's `__TEXT,__info_plist` section through a cgo `-Wl,-sectcreate` linker flag, which the Go cgo allowlist accepts. A packaged bundle's `Contents/Info.plist` still takes precedence. The packaging Info.plist also gains `CFBundleDisplayName`. The window title stays "Agent Marketplace".

## Source names

`config.SourceDescription` gains the v4 source name (the `sources:` key) and the local API source description exposes it as `name`. The opaque source `id` keeps its current inputs, so it does not change. The sidebar labels a Marketplace source by that name unless it is empty or the generic GUI-assigned `marketplace` / `marketplace-<n>`, in which case it shows the endpoint host.

## Disabled indicator

The local API row gains a field that marks a disabled resource. The row check dot renders that state in the warning role; the signal role stays reserved for resources that are on.

## Source tooltip

The native `title` tooltip follows the operating system appearance, not the app theme, so it cannot meet contrast when the two differ. Source rows use a themed tooltip element instead; the accessible name keeps the full location.

## Add-source errors

Add-source request failures are reported through the controller's source error, which the Add source section already renders, instead of the Settings-wide action error. Mutation error diagnostics from an add-source operation started in the form also render in that section.
