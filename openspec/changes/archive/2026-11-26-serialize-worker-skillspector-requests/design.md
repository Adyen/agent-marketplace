# Design

## Context

See `proposal.md` for the failure mode. A scanner instance uses one `http.Transport` for requests to its configured endpoint, while the worker has one scan slot.

## Goals / Non-Goals

**Goals:**
- Queue requests made through one scanner instance while its worker slot is occupied.
- Preserve request contexts, timeouts, and existing report interpretation.

**Non-Goals:**
- Coordinate separate client processes.
- Add retries, increase scan limits, or introduce a scheduler.

## Decisions

Set `MaxConnsPerHost` to one on the scanner's existing transport. The transport queues concurrent requests for the endpoint before they reach the worker. This keeps the serialization local to the existing scanner and lets current request deadlines bound queue time; an application-level scheduler or retry policy would add unnecessary state and alter behavior.

## Risks / Trade-offs

- A queued request can use its existing deadline while waiting and may time out before reaching the worker; this is intentional and keeps the fixed scan budget unchanged.
