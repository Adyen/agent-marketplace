## Context

Symbolic extraction uses the captured parent but calls native RemoveAll without leaf admission. A regular destination hardlinked to another path is consumed even though ordinary file restore refuses that shape.

## Decisions

Inspect structural metadata below captured native root, normalize genuine absence, preserve directory/symbolic replacement semantics and refuse special leaves. Existing regular leaf uses shared parent.OpenFile no-follow/nonblocking regular/single-link validation, checks classified full state and retains descriptor through mutation/cleanup. No target payload read or link following. Propagate close failures and preserve source comments. This slice closes shape admission, not later original-generation leases, whole-tree snapshot/transaction or uncooperative-writer exclusion.

## Verification

Actual public Create archives an inert symbolic target, replace actual native leaf with hardlink to foreign inert bytes, then public Restore: parent removed/replaced multiply linked leaf/red; corrected typed unsafe-file refusal keeps destination inode/mode/mtime and foreign bytes/green. Removing the extra fixture link lets real Restore publish exact archived target text. Linux/macOS full backup suites/count20 consumers/validate/lint/build/spec pass. Eight implementation files reviewed incl public test/script/spec exclusions, none skipped; opened regular shape/state and nonshadowed close-error propagation checked. Existing symbolic namespace disk replacement benchmark93.381→141.081µs(+51.08%),168→408B(+142.86%),9→12alloc(+33.33%),2sx5 p=.008/no95CI. Measures existing symbolic-leaf remove/create, not full archive decode/regular admission; opened regular descriptor follow-up does not affect that timed path. Cost not speedup. Initial root/parent aliases are explicitly supported by the normative archive-generation requirement; no blanket alias ban added.
