## ADDED Requirements

### Requirement: Overlapping SkillSpector scans queue within their existing budgets

Overlapping SkillSpector requests through the same scanner instance for its configured single-slot worker MUST wait for an available slot rather than fail as unavailable solely because the slot is occupied. Requests MUST retain their existing deadlines and scan budgets; a queued request that expires before reaching the worker MUST remain incomplete, not become a successful report with no findings. This behavior does not coordinate separate scanner instances or client processes.

#### Scenario: overlapping indexes share one worker scanner

- **GIVEN** a SkillSpector worker with one scan slot is processing an index request
- **WHEN** another index request starts before the first scan completes
- **THEN** the second request waits for the worker slot within its existing deadline
- **AND** if its deadline expires while queued, it is not reported as a complete scan with no findings
- **AND** a later queued request that reaches the worker within its deadline receives its own supported report
