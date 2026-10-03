## Context

Rotate previously opens backup target names through independent paths and removes ordinary archives by timestamp alone. A hardlinked old archive is deleted rather than refused. Safe regular retention controls define the existing two-axis ordering.

## Decisions

Capture the backup directory and enumerate via os.Root.FS; ordinary target traversal is strict below that descriptor. Preserve selected names and policy ordering but plan all selected removals before mutation. Each selected member is admitted through CaptureDestination and opened ReadTargetWith validation without payload decoding; refuse links/nonregular/multiple links. Store identities, not open descriptors, to avoid descriptor accumulation for large retention sets. Before each unlink verify named target parent, recapture/open the selected member and compare full state. Descriptor-relative Unlink does not add durability sync. Do not claim a batch transaction or exclusion of uncooperative writers. Codex set retention still uses its existing separate lane and remains open.

## Verification

Public Rotate hardlinked old archive acceptance reproduces/red; typed unsafe-file refusal preserves old/newest/foreign bytes/green. Remove the extra hardlink and public rotation actually deletes the old day while retaining newest bytes. Deterministic observed-member replacement refuses typed generation identity and preserves replacement bytes. Existing day/per-day/two-axis controls, Linux/macOS full backup/count20 rotation and member-generation races/validate/lint/build/strict specs pass. Review10/10implementation files includes test/benchmark/script/spec exclusions, none skipped; review replaced retained member descriptors with metadata identities and per-inspection closure, retaining original comments and policy order. Same public disk rotation benchmark, local/no network/seven archive writes per iteration plus rotation from seven to five retained days:717.940→1875.183µs, baseline9247B/90alloc versus37518B/442alloc, five2s samples/no95%CI; final1.79–1.97ms varied under validation load. Includes fixture writes and real cleanup, not compressed payload decoding or Codex sets; validation cost, not a speedup.
