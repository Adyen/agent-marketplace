# Design

## Context

The local API writes and flushes each server-sent event, and the GUI's frame parser handles complete frames, yet completion notifications appear late in the desktop app. The desktop app reaches the local API through the webview's asset-server transport rather than a loopback socket, so delivery depends on that transport as well as the server and parser. Diagnostics discovery health is currently carried by the query-scoped discovery projection, which is reset whenever the resource search changes.

## Goals / Non-Goals

**Goals:**
- Root-cause each reported defect with a failing test before fixing it.
- Keep fixes inside the GUI, its local API, and its application host.

**Non-Goals:**
- Section order, Space key, post-install selection, and Update/Sync redesign.
- Visual redesign.
- CLI or TUI behavior changes beyond a shared-core root-cause fix.

## Decisions

- Prompt delivery is specified at the GUI boundary, so the fix may live in the server, the transport, or the client, wherever the root cause is proven.
- Discovery health shown in Diagnostics comes from configuration-wide discovery state rather than the query-scoped search result.
- The install-preparation failure and post-install consistency are specified only once their root causes are known; a fix that needs a product decision is recorded for follow-up instead of chosen here.

## Risks / Trade-offs

- A transport-level buffering cause cannot be reproduced in a headless browser; its regression test must target the layer that is fixed.
