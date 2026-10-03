## Context

isManagedCommandFile is used by native single-file overwrite and stale prune consumers, but reads the marker via os.ReadFile. Native Factory command update accepts a multiply linked marker and replaces output; FIFOs may block the same consumer path.

## Decisions

Capture the parent and strict destination. Do not follow a symbolic marker leaf, require a regular singly linked nonblocking descriptor and retain captured-size/read-state/named-target validation. Parse the unchanged leading frontmatter marker from admitted bytes. Keep the bool reader/collision.user-owned contract so an unreadable object never grants ownership; close failures also return no ownership. No new input ceilings/whole-file format changes. A safe marker read is not publication/prune authority for a later generation; those callers remain next layers.

## Verification

Actual native original command and marker payload precede a hardlink update that is red/green: original reader updates it; guarded reader yields collision and preserves native/foreign bytes. FIFO probe uses a bounded subprocess through actual native Run and proves the named pipe/retained payload remain intact. Linux/macOS/full short materialize/count20 native hardlink/FIFO races/scoped validate/lint/build and strict specs pass. Actual valid update payload is checked before refusal; native/retained identity/mode/mtime and bytes remain intact. All nine implementation entries reviewed including test/benchmark/script/spec exclusions with no skips; all three marker callers, FD scope/shape/read/cleanup and bool-collision error semantics checked. Review prevents cleanup failures from proving ownership; original comments retained. Marker local-disk/no-network reader2s×5 arm64 median21.336→152.648µs(+615.45%),1096→6608B(+502.92%),8→74alloc(+825%),p=.008n5/no95%interval. Added validation cost—not lifecycle speedup. Final R2 integrated/comment gate remains queued.
