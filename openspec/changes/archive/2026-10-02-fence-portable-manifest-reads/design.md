## Context

loadPortableState runs during target discovery and target execution. It decodes manifest ownership from a path read. Existing entry validation and ownership decisions are unchanged by this reader slice.

## Decisions

1. Use the canonical captured repository/HOME anchor and root-directory policy for the existing manifest parent. Contained parent aliases remain valid; escaping parents refuse. Do not create missing parents when loading.
2. Open the manifest leaf directly with shared no-follow/nonblocking regular singly-linked admission. Read from that descriptor with a captured-size limit+1 bound and compare admitted/read identity, including ctime, mode and links.
3. Keep absent manifest and registered materialize.manifest.error behavior. Do not echo payload bytes or parse refused content. No new absolute size ceiling.
4. Scope is reading ownership claims. Later publication/removal and portable output mutation still require their own reviewed ports; no whole-installation transaction or installed compiled-cache preservation claim is made here.

## Verification

An actual native Factory file-mode update imported aliased ownership on the parent and failed the new refusal witness; corrected reads preserve native and foreign bytes/identity/mode/mtime and the manifest alias. Review found an unsafe dangling parent wrapped with not-found identity could be treated as absence: the actual read-only regression failed before error-precedence correction and now refuses without creating output. Contained repository/machine parents retain real native updates/checks; hard-linked/directory/escaping-parent controls refuse in both scopes. Built CLI v2/v4 install/sync/check, Linux/macOS, scoped validate/lint/build, count-20 shared security races, strict specs and final combined E2E/configuration gate plus shellcheck pass. All ten implementation entries, including exclusions, were adversarially reviewed. Existing disk/no-network portable lifecycle benchmark, 2s ×5, reports median 6.231ms → 6.516ms (no significant latency difference, p=1.000), 395939 → 407322 B/op (+2.87%) and 3943 → 4097 allocs/op (+3.91%), allocation p=0.008. Five samples do not provide a 95% interval; host variance exists and this is not a performance improvement claim.
