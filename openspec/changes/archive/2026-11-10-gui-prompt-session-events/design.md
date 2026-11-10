# Design

## Evidence

Native Linux WebKitGTK 4.1 runs under Xvfb, using the packaged build tags `desktop,production,webkit2_41`. A loopback control subscriber received every frame within 10 ms. Over the Wails custom-scheme fetch stream, the window stalled after the first progress frames, and the remaining frames arrived only when the next request was made. Events emitted with the Wails runtime event bridge arrived within 46 ms in all 123 cases. The mechanism inside Wails/WebKitGTK is out of scope: Wails and vendored code stay unchanged.

## Decisions

- `internal/localapi` exposes one in-process subscription that both the HTTP `/api/events` handler and the desktop host use. Both paths therefore emit identical frames: the current snapshot first, then live events in order.
- The desktop host listens for a window request to open a stream (stream id plus credential) and to close it. It validates the credential in constant time. For each frame, it emits the SSE-framed bytes to the window with the stream id. When the subscription ends, it emits an end signal for that id. An invalid credential gets an unauthorized end signal and no subscription.
- When the Wails runtime is present, the GUI client reads events from the bridge. The frames go through the existing SSE parser and `EventCursor`, so ordering and stale-event rules are unchanged. Otherwise the client uses the HTTP stream. An unauthorized end signal surfaces as the same 401 `RequestError` the HTTP stream raises.
- No heartbeat is added, and no new bound Go methods are added.
