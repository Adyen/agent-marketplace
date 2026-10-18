## Decision

Define one package-level sentinel per refusal whose text is its diagnostic
code, and wrap it with `fmt.Errorf("%w: ...")`, matching
`ErrMarketplaceInstallCollision`. The CLI therefore prints `<code>: <message>`
like other Marketplace diagnostics.

A typed error that kept the previous message byte-for-byte was rejected: it
would add a second diagnostic style for one command, while codes are already
treated as public behavior in this form.

The ambiguity refusals for kind, source, and scope predate this change, carry no
code, and expose their remediation text as the contract; they are unchanged.

## Verification

The refusal test asserts each case with `errors.Is` and keeps its unchanged
configuration and lock assertions. It must fail while the refusals are not
wrapped.
