## 1. Reference

- [x] 1.1 Cut connective prose on reference pages where it was present: the glossary, the configuration layers introduction, and the TUI reference. Rewrite the refusal rules in the CLI and configuration references in plain English. Tables, rules, and code stay unchanged.

## 2. Explanation

- [x] 2.1 Cut explanation prose to plain English without losing design rules or caveats.

## 3. Consumer how-to

- [x] 3.1 Shorten the consumer how-to pages, keeping step order and expected results. Pages with lead-ins and restated output shrank 15 to 25 percent; pages that are mostly facts changed little. The refusal rules in the install, remove, and migration guides are rewritten in plain English.

## 4. Author how-to

- [x] 4.1 Shorten the registry author how-to pages, keeping step order and expected results. Most pages changed only a few sentences; `publish-resource-collections.md` was left as it was because it was already tight.

## 5. Operator how-to

- [x] 5.1 Shorten the operator how-to pages, keeping step order and expected results. Changes are limited to intros, lead-ins, and repeated sentences; configuration, command, and table content is unchanged.

## 6. Tutorials

- [x] 6.1 Shorten the tutorials, keeping every step, expected output, and undo instruction. Changes are limited to overview sentences and wording.

## 7. Top-level pages

- [x] 7.1 Shorten `index.md`, `install.md`, `quick-start.md`, and `terminal-basics.md`.

## 8. Verify

- [x] 8.1 Run the code-block invariant check against the base commit for every changed page.
- [x] 8.2 Run the site build without dead links.
- [x] 8.3 Run `make spec-check` and `TestDocsContract`.
- [x] 8.4 Archive with `openspec archive plain-language-docs-pass --yes`.
