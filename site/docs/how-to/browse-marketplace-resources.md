# Browse and inspect Marketplace resources

Use the hosted [Marketplace](../glossary.md#marketplace) browser to find a resource and read the exact revision before you install it. Browsing never changes your local configuration. You install from your terminal.

## 1. Find a candidate

Search the resource browser by text. Narrow the results by kind or by one or more registries, and remove a filter chip or clear all filters to widen them. The page URL holds your search and filters, so you can bookmark or share it.

When the operator connects an inventory metrics endpoint, browsing orders resources by reported adoption. A text search still puts the most relevant matches first. Rows may show a "reported installations" count. Treat it as a lower bound: it counts only installations that report telemetry. A row without a count means no count is available, not zero use. The [HTTP API reference](../reference/marketplace-http-api.md) documents the exact count semantics.

Scroll to load more results. If a request fails, retry it; the results already on the page stay visible. Check the registry and kind as well as the name, because one name can appear in different kinds and sources.

## 2. Inspect before installing

Open a result and use the detail tabs:

| Tab | What to check |
|---|---|
| Content | Read the main instructions. Use **Inspection file** to open another available file, and **Preview** / **Raw** to switch between rendered content and source text. A collection lists the resources it includes. |
| Security | Read the automated assessment and its findings. Some findings link to the highlighted text they refer to. A scan that found no issues does not guarantee the resource is safe. |
| Revisions | Open an older revision, or follow **Newer revision available** to the current one. After you switch, check the content and findings again. |
| Registry | See who published the resource and where it came from. You can also browse the registry's other resources and its history. |
| Technical details | Check the full content digest, the acquisition evidence, and the declared license. Use these to confirm where the resource came from or to report a problem. |

The browser previews only the files made available for safe inspection. If it cannot show a file, do not assume the file is empty or harmless.

You can inspect a revoked or policy-rejected revision, but its install action is disabled. A revision can also lack the artifact that installation needs. The availability notice on the page explains why.

## 3. Choose a revision and copy its command

The command under **Install** pins the revision you are viewing. Copy it to install exactly that revision, or share the revision's page URL so someone else can inspect the same content. A resource URL without a revision lets the CLI pick a newer usable revision later.

Before you run the command, [install the CLI](../install.md), choose your target [harnesses](../glossary.md#harness), and check that the CLI can edit the configuration file it will write to. You do not need to register the Marketplace endpoint first to install from a browser URL. See the [configuration compatibility note](../explanation/user-journeys.md#configuration-compatibility).

Run the copied command inside the repository where you want the resource. By default it installs into the repository scope. To choose another scope, add `--scope`:

```text
agent-marketplace install <copied-revision-url> --scope user
agent-marketplace install <copied-revision-url> --scope local
```

Run only one of these. `user` installs the resource for all your projects on this machine, and `local` keeps it personal to this checkout. To share it through the repository configuration, use `--scope repo`. Each command ends with `✓ Installed <name>` on success. [Hosted installation](./install-from-a-marketplace.md#_3-install-the-resource) shows the full output and the risk prompt that can appear first.

## 4. Review the verified CLI result

The CLI verifies the artifact before it writes configuration, even after you inspected the resource in the browser. Read the findings and capabilities it prints. Security findings make the CLI ask for confirmation; capabilities alone do not. If you decline, the CLI does not install. The prompt cannot approve a blocked or revoked revision.

After installation, run:

```bash
agent-marketplace status
agent-marketplace sync --check
```

`status` prints the `MARKETPLACE PINS` table, where the new pin's `STATE` should read `current`. `sync --check` prints nothing apart from any warnings and exits `0` when the checkout matches. Check that the revision, the configuration layer, and the harness targets are the ones you chose. Then open or reload your harness. If installation fails or the name conflicts with another resource, see [hosted installation troubleshooting](./install-from-a-marketplace.md#troubleshooting).

## Continue the journey

- [First Marketplace skill tutorial](../tutorials/install-your-first-marketplace-skill.md): a complete CLI setup.
- [Hosted installation guide](./install-from-a-marketplace.md): endpoint registration, risk consent, and local aliases.
- [Resource locators](../reference/marketplace-resource-locators.md): exact URL and coordinate syntax.
- [User journeys](../explanation/user-journeys.md): sharing, updating, removing, and recovering resources.
