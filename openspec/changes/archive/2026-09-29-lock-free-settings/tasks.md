## 1. Settings behavior

- [x] 1.1 Keep logging and telemetry settings independent of remote lock acquisition; verify `TestLoadUserLoggingConfigIgnoresUnreadableLock` and `TestLoadAndResolveSettingsIgnoresUnreadableLock` pass.

## 2. Bind and archive

- [x] 2.1 Annotate the unreadable-lock test with the configuration-settings scenario; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused telemetry tests; verify both commands pass.
- [x] 2.3 Run `openspec archive lock-free-settings --yes` as the final change in this pull request; verify the archived spec is present.
