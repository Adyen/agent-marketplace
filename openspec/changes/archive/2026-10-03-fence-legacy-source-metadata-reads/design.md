## Context

Legacy DiscoverSkills parses SKILL.md via path-open; DeclaredNameFromFile reopens a canonical diagnostic path. A foreign linked SKILL.md supplies a valid native name/description to the real legacy discovery consumer. Best-effort recovery and frontmatter scans already bound work by scanner line/byte policies and early delimiters.

## Decisions

1. Add a streaming read callback to captured destinations for actual bounded-prefix consumers, without exposing raw destination descriptors. Strict regular/single-link/nonblocking open and captured-state verification surround the callback; the io.Reader is limited by captured size and closed at the boundary.
2. Read legacy prefix metadata through one shared scoped helper, preserving frontmatter and best-effort scan policies and syntax semantics. Do not buffer whole bodies or add an arbitrary maximum size.
3. Discovery passes its registry root; malformed correlation passes the admitted real root. The original standalone best-effort helper remains compatible and roots a file at its containing resource directory; contained file aliases stay admissible, foreign payload metadata does not.
4. Existing comments, full ReadTarget semantics and unrelated readers/preview/mutation consumers remain unchanged. No whole-tree source atomicity.

## Verification

Public legacy DiscoverSkills foreign-source positive/refusal witness is red/green; name recovery also returns no foreign name. Contained authored file aliases, hardlinks, bounded actual FIFO discovery/name recovery, deterministic rewrite and alias-retarget during prefix callbacks retain typed refusals and preservation. Existing malformed/scanner/metadata/early-termination contracts pass, as do Linux/macOS/count20 races/full affected short skill/fsboundary/catalog/pipeline, validate/lint/build and strict specs. All15 implementation entries including excluded tests/script/spec files reviewed with none skipped; callback descriptor lifetime, captured-size stream limits, full-state plus final hierarchy/link/leaf checks, scoped roots, syntax and best-effort disposition checked. Final R2 integrated/GitHub passes remain pending until delivery. Existing tiny-frontmatter disk prefix scan benchmark2s ×5 arm64 median27.102→189.279µs(+598.39%),11376→17320B/op(+52.25%),112→185alloc(+65.18%),p=.008 n5/no95%interval. Descriptor validation cost, not speedup or whole-body-read/whole-lifecycle benchmark.
