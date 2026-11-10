# Tasks

## 1. Window event bridge

- [ ] 1.1 Add failing tests bound to `gui-local-api/completion-notification-renders-without-further-activity` (client bridge delivery and native Linux window) and `gui-local-api/window-event-stream-requires-the-credential`
- [ ] 1.2 Share one in-process session event subscription between the HTTP event stream and the desktop host
- [ ] 1.3 Forward session events to the desktop window over the Wails runtime event bridge with credential validation
- [ ] 1.4 Read session events from the bridge in the GUI client when the Wails runtime is present
- [ ] 1.5 Run the native Linux window regression in CI
