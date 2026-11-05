# Temporary Security Patches

These patches apply during every pnpm install, including frozen installs with
scripts disabled and the web container build. Their hashes are recorded in
`pnpm-lock.yaml`; keep the patch files in the Docker dependency-install layer.

| Package | Advisory | Patch |
| --- | --- | --- |
| `braces@3.0.3` | [CVE-2026-93687](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm) | Reject parsed AST nesting deeper than 100 before recursive walkers run. The bound covers both braces and parentheses, without counting escaped or quoted delimiters. |
| `node-forge@1.4.0` | [CVE-2026-85393](https://github.com/advisories/GHSA-86w9-cpqp-85rv) | Check the nested DigestAlgorithm element count as well as the outer DigestInfo count, following [upstream PR #1152](https://github.com/digitalbazaar/forge/pull/1152). |

The `@nuxt/devtools@3.4.0` compatibility patch changes its `simple-git` default
import to the named `simpleGit` export required by the patched v4 release.
Remove it when the consumer supports v4 natively.

Neither package has a fixed npm release as of 2026-10-22. Version-based scanners,
including Dependabot and `pnpm audit`, still report these two versions. The
patches mitigate the reported flaws; they do not justify hiding the advisories.
Replace each patch with a verified fixed release when one becomes available.

Run `pnpm exec vitest run tests/unit/dependency-security.test.ts` from `web/`.
The regressions exercise the installed dependency paths, reject malicious
inputs, and retain valid brace expansions and RSA signatures with and without
NULL algorithm parameters.
