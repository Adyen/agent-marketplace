## Context

An EXIT trap can run after a background creator starts but before its PID is
assigned. At that point neither the PID nor the pending container ID is
available to cleanup. An analogous startup window exists for the creation
watchdog.

## Decisions

During creation setup, record INT/TERM cancellation instead of exiting.
Restore ordinary signal handling after both PIDs are assigned, then honor any
recorded cancellation through the existing cleanup path. Keep process-group
isolation, the creation deadline and container-ID ownership checks unchanged.

The regression interposes a signal immediately before creator PID assignment
in a fixture copy of the launcher. Direct and group signals must return 143,
remove exactly the owned container ID and never start the cancelled container.
Both cases fail without the fix. No timing delay or product test hook is added.
