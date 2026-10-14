## Context

The open CodeQL results on main e5c71c05 share a small number of trust boundaries. Blob keys currently reach filesystem paths directly; inspection checks omit data and vbscript; float-to-int validation admits the rounded upper boundary; logging relies on downstream handler encoding. Other findings concern allocation hints and test-only URL/file operations.

## Goals / Non-Goals

Goals: close unsafe boundaries with standard-library operations, preserve valid output and storage identities, and exercise rejection plus positive controls through consumers.

Non-goals: changing SHA-256 content identities, slowing identity hashes as though they were password storage, suppressing queries, or weakening security policy.

## Decisions

- Use os.Root for anchored blob reads and publication, preserving the existing digest layout and atomic immutable publication.
- Use filepath.IsLocal alongside existing artifact validation for explicit archive containment.
- Check integer bounds before conversion, including the exclusive floating-point upper boundary, and compare int64 values against constant native-int bounds.
- Share only CR/LF attribute encoding in a dependency-light logging value package so telemetry configuration does not acquire a configuration import cycle.
- Use bounded capacity hints rather than unchecked sums; append retains runtime overflow checks.
- Set proxy URL pathname/search on a configured origin rather than resolving user path text as a URL reference. Read QA files through one no-follow descriptor validated by fstat.

## Risks / Trade-offs

Descriptor anchoring adds filesystem overhead; focused benchmarks quantify it. Capacity hints may grow more often, so existing merge and JSONC benchmarks are measured before and after. CodeQL closure requires a new analysis of the changed revision; tests do not substitute for that analysis.
