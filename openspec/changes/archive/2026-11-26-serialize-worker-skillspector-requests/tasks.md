# Tasks

## 1. Regression coverage

- [x] 1.1 Add the annotated overlapping-index regression at the scanner/report boundary; verify it fails before the transport change because concurrent requests reach the single-slot worker or report unavailable.

## 2. Implementation

- [x] 2.1 Limit each scanner transport to one connection per host; verify the focused concurrency tests pass with existing deadlines and distinct reports.

## 3. Verification and archive

- [x] 3.1 Run the focused advisory test, `make spec-check`, and the requested scoped `make validate`; record each result.

## Workflow follow-up

- Archived as `2026-11-26-serialize-worker-skillspector-requests` after syncing the `marketplace-analysis` delta to the main specification.
