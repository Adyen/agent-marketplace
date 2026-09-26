# Browse and inspect Marketplace resources

Use the hosted browser to find a resource and read the revision you want to install. Installation happens in your terminal; browsing leaves your local configuration unchanged.

## 1. Find a candidate

Open your Marketplace's resource browser and search by text. You can narrow the results to a resource kind or one or more registries. To broaden the search, remove a filter chip or clear the filters. The URL includes your search and filters, so you can bookmark or share it.

Scroll to load more results. If a request fails, retry it; the results already on the page stay visible. Check the registry and kind as well as the name, since names can repeat across kinds and sources.

## 2. Inspect before installing

Open a result and use the detail tabs:

| Tab | What to check |
|---|---|
| Content | Read the main instructions. Use **Inspection file** to choose another available file, and **Preview** / **Raw** to switch between rendered content and source text. Collections list their included resources. |
| Security | Read the automated assessment and findings. Some findings link to highlighted text. A scan with no detected issues does not guarantee safety. |
| Revisions | Open a historical revision, or follow **Newer revision available** to the current one. Check its content and findings after switching. |
| Registry | Check who published the resource and where it came from. You can also browse the registry's other resources and history. |
| Technical details | Check the full content digest, acquisition evidence, and declared license to verify where the resource came from or report a problem. |

The browser can preview only files made available for safe inspection. If it cannot show a file, don't assume the file is empty or harmless.

You can still inspect revoked or policy-rejected revisions, but their install action is disabled. A revision may also be missing the artifact needed for installation. The availability notice explains why you cannot install it.

## 3. Choose a revision and copy its command

The command in **Install with the CLI** pins the revision you're viewing. Copy it to install that revision, or share the revision-specific page URL so someone else can inspect the same content. An unversioned resource URL allows the CLI to choose a newer usable revision later.

Before running the command, [install the CLI](../install.md), select your target harnesses, and check that it can edit the destination config. You can install from a browser URL without registering the endpoint first. See the [configuration compatibility note](../explanation/user-journeys.md#before-following-a-journey).

Run the copied command inside the repository where you want the resource. To change the default repository scope, add a scope to the command:

```text
agent-marketplace install <copied-revision-url> --scope user
agent-marketplace install <copied-revision-url> --scope local
```

Choose one command: `user` installs machine-wide, while `local` keeps the selection personal to this checkout. To share the selection through repository config, use `--scope repo`.

## 4. Review the verified CLI result

The CLI verifies the artifact before writing configuration, even if you already inspected it in the browser. Review its findings and capabilities; security findings trigger a confirmation prompt, while capability metadata alone does not. If you decline, the CLI does not apply the prepared installation. The prompt cannot approve a blocked or revoked revision.

After installation:

```bash
agent-marketplace status
agent-marketplace sync --check
```

Check that the revision, config layer, and harness targets match what you chose, then open or reload your coding harness. If installation fails or a name conflicts, see [hosted installation troubleshooting](./install-from-a-marketplace.md#troubleshooting).

## Continue the journey

- [First Marketplace skill tutorial](../tutorials/install-your-first-marketplace-skill.md): a complete CLI setup.
- [Hosted installation guide](./install-from-a-marketplace.md): endpoint registration, risk consent, and local aliases.
- [Resource locators](../reference/marketplace-resource-locators.md): exact URL and coordinate syntax.
- [User journeys](../explanation/user-journeys.md): sharing, updating, removing, and recovering resources.
