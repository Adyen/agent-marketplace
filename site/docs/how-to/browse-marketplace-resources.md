# Browse and inspect Marketplace resources

Find a resource in the hosted browser, inspect the exact revision, and hand off to the CLI for installation. Browsing does not change your local configuration or install anything.

## 1. Find a candidate

Open your Marketplace's resource browser. Search by text, choose a resource kind, and filter by one or more registries. Remove a filter chip or clear filters to broaden the results. Search and filters are reflected in the URL, so you can bookmark or share that view.

Scroll to load additional results. If a request fails, use the retry action; already loaded results remain visible. A resource's name alone is not its identity: check its registry and kind before opening it.

## 2. Inspect before installing

Open a result and use the detail tabs:

| Tab | What to check |
|---|---|
| Content | Read the main instructions. Choose another **Inspection file** when available; switch **Preview** / **Raw** for rendered content or source text. Collections expose their included resources. |
| Security | Review the automated assessment and findings. Follow highlighted finding ranges where available. No detected issues is not a safety endorsement. |
| Revisions | Open a specific historical revision, or follow **Newer revision available** to the current one. Recheck content and findings after switching. |
| Registry | Check the source and publisher details; browse other resources from that registry or its history. |
| Technical details | Inspect the full content digest, acquisition evidence, and declared license when you need to verify provenance or report a problem. |

Only files made available for safe inspection can be previewed. Unavailable content is not evidence that the file is empty or harmless.

Revoked and policy-rejected revisions remain inspectable, but their install action is disabled. A revision can also lack an installable artifact; read the availability notice rather than assuming that a visible result can be installed.

## 3. Choose a revision and copy its command

The **Install with the CLI** panel targets the exact revision currently displayed. Copy its command, or share the revision-specific page URL so another person can inspect the same content. Copying only the resource's unversioned URL allows the CLI to resolve a newer usable revision later.

You do not need to register the endpoint before installing from a browser URL. You do need the [CLI installed](../install.md), a configured target harness set, and an editable destination layer. See the [configuration compatibility note](../explanation/user-journeys.md#before-following-a-journey).

Run the copied command inside the intended repository. Add an explicit scope if you do not want the default repository installation:

```text
agent-marketplace install <copied-revision-url> --scope user
agent-marketplace install <copied-revision-url> --scope local
```

Choose **one** command: `user` installs machine-wide; `local` is personal to the current checkout. For shared repository selection, use `--scope repo`.

## 4. Review the verified CLI result

The CLI verifies the artifact before writing configuration. Browser metadata is not a substitute for this check. Review any warning findings and active capabilities before answering the confirmation prompt. Declining leaves that prepared installation unapplied; a blocked or revoked revision cannot be approved through the prompt.

After installation:

```bash
agent-marketplace status
agent-marketplace sync --check
```

Confirm the intended revision, source layer, and harness targets, then open or reload your coding harness. If installation is rejected or a name conflicts, follow [hosted installation troubleshooting](./install-from-a-marketplace.md#troubleshooting).

## Continue the journey

- [First Marketplace skill tutorial](../tutorials/install-your-first-marketplace-skill.md): a complete CLI setup.
- [Hosted installation guide](./install-from-a-marketplace.md): endpoint registration, risk consent, and local aliases.
- [Resource locators](../reference/marketplace-resource-locators.md): exact URL and coordinate syntax.
- [User journeys](../explanation/user-journeys.md): sharing, updating, removing, and recovering resources.
