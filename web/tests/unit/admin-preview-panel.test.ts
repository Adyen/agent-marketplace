import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import AdminPreviewPanel from "../../app/components/AdminPreviewPanel.vue";
import {
  AdminError,
  newAdminRegistry,
  previewIsStale,
  registryDraftKey,
} from "../../app/lib/admin";
import type {
  AdminPreview,
  AdminPreviewState,
  AdminRegistry,
} from "../../app/lib/admin";
import { adminStubs } from "./admin-stubs";

const registry: AdminRegistry = {
  ...newAdminRegistry(),
  id: "team",
  slug: "team",
  origin: "https://git.test/org/repo",
  roots: [
    { path: "skills", source_harness: "" },
    { path: "docs", source_harness: "" },
  ],
};
const pending: AdminPreview = {
  id: "p1",
  marketplace_id: "mp",
  actor: { issuer: "sso", subject: "alice" },
  base_revision: 3,
  registry,
  existing: true,
  job_id: "managed-preview-p1",
  status: "pending",
  created_at: "2026-09-19T12:00:00Z",
};
const succeeded: AdminPreview = {
  ...pending,
  status: "succeeded",
  completed_at: "2026-09-19T12:00:05Z",
  result: {
    commit: "abcdef1234567890abcdef1234567890abcdef12",
    ref: "main",
    observed_at: "2026-09-19T12:00:05Z",
    policy_revision: 1,
    diff_base: "pub-1",
    total_resources: 2,
    roots: [
      {
        path: "skills",
        resource_count: 2,
        resources: [
          {
            kind: "skill",
            name: "review",
            source_path: "skills/review/SKILL.md",
            description: "Review code",
            decision: "allow",
            change: "added",
          },
          {
            kind: "command",
            name: "lint",
            source_path: "skills/lint.md",
            decision: "warn",
            findings: 2,
            change: "unchanged",
          },
        ],
      },
      { path: "docs", resource_count: 0, resources: [] },
    ],
    removed: [{ kind: "subagent", name: "reviewer", change: "removed" }],
    diagnostics: [
      {
        root: "skills",
        code: "ingest.factory.bundle-manifest-synthesized",
        message: "manifest synthesized",
      },
    ],
  },
};
const wrappers: ReturnType<typeof mount>[] = [];
function render(
  modelValue?: AdminPreviewState,
  draft: AdminRegistry = registry,
) {
  const wrapper = mount(AdminPreviewPanel, {
    props: {
      registry: draft,
      revision: 3,
      modelValue,
      "onUpdate:modelValue": (value: AdminPreviewState) =>
        wrapper.setProps({ modelValue: value }),
    },
    global: { stubs: adminStubs },
  });
  wrappers.push(wrapper);
  return wrapper;
}
function previewButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => /Preview (discovery|again)/.test(button.text()))!;
}
function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => {
  wrappers.forEach((wrapper) => wrapper.unmount());
  wrappers.length = 0;
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("admin discovery preview panel", () => {
  it("requests a preview for the exact draft and polls until it completes", async () => {
    vi.useFakeTimers();
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response(pending, 202))
      .mockResolvedValueOnce(response({ ...pending, status: "running" }))
      .mockResolvedValueOnce(response(succeeded));
    vi.stubGlobal("fetch", fetcher);
    const wrapper = render();
    expect(wrapper.text()).toContain("Preview discovery");
    await previewButton(wrapper).trigger("click");
    await flushPromises();
    const [url, init] = fetcher.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/admin/previews");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["If-Match"]).toBe('"3"');
    expect(JSON.parse(init.body as string)).toMatchObject({
      action: "upsert_registry",
      registry_id: "team",
      registry: { roots: registry.roots },
    });
    expect(wrapper.find('[aria-busy="true"]').text()).toContain(
      "Waiting for a worker",
    );
    await vi.advanceTimersByTimeAsync(3000);
    await flushPromises();
    expect(fetcher.mock.calls[1]?.[0]).toBe("/api/v1/admin/previews/p1");
    expect(wrapper.find('[aria-busy="true"]').text()).toContain(
      "Discovering resources",
    );
    await vi.advanceTimersByTimeAsync(3000);
    await flushPromises();
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("resources atabcdef123456");
    expect(fetcher).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("renders roots, empty-root warnings, change markers, removals and diagnostics", () => {
    const wrapper = render({
      preview: succeeded,
      registry: registryDraftKey(registry),
    });
    const text = wrapper.text();
    expect(text).toContain("abcdef123456");
    expect(text).toContain("1 Skill");
    expect(text).toContain("1 Command");
    expect(wrapper.find('[role="status"]').text()).toContain(
      "One root exposed no resources: docs",
    );
    const roots = wrapper.findAll("ol > li");
    expect(roots).toHaveLength(2);
    expect(roots[0]!.text()).toContain("review");
    expect(roots[0]!.find('[data-color="success"]').text()).toBe("New");
    expect(roots[0]!.find('[data-color="warning"]').text()).toBe("Warnings");
    expect(roots[0]!.text()).not.toContain("Unchanged");
    expect(roots[1]!.text()).toContain("Nothing discoverable under this root");
    expect(text).toContain("Currently published but no longer discovered");
    expect(text).toContain("reviewer");
    expect(text).toContain("1 catalog diagnostic");
    expect(text).toContain("1 new");
    expect(text).toContain("1 removed");
    expect(text).toContain("Preview again");
  });

  it("marks a preview stale once the draft changes and explains failures without raw output", () => {
    const state = { preview: succeeded, registry: registryDraftKey(registry) };
    const edited = {
      ...registry,
      roots: [{ path: "other", source_harness: "" }],
    };
    expect(previewIsStale(state, registry)).toBe(false);
    expect(previewIsStale(state, edited)).toBe(true);
    const stale = render(state, edited);
    expect(stale.find('[role="status"]').text()).toContain(
      "draft changed since this preview",
    );

    const failed = render({
      preview: {
        ...pending,
        status: "failed",
        failure: {
          code: "catalog_error",
          message: 'load catalog at "docs": no recognizable layout',
        },
      },
      registry: registryDraftKey(registry),
    });
    const alert = failed.find('[role="alert"]');
    expect(alert.text()).toContain("no recognizable layout");
    expect(alert.text()).toContain('load catalog at "docs"');
    expect(alert.text()).toContain("catalog_error");
    expect(failed.find('[aria-busy="true"]').exists()).toBe(false);
  });

  it("shows valid resources alongside skipped-skill warnings", () => {
    const preview = {
      ...succeeded,
      result: {
        ...succeeded.result!,
        diagnostics: [
          {
            root: "skills",
            code: "ingest.factory.load-skill-failed",
            message: "Skipped malformed skill: skills/broken/SKILL.md",
            error: false,
          },
        ],
      },
    };
    const wrapper = render({ preview, registry: registryDraftKey(registry) });
    expect(wrapper.text()).toContain("Preview completed with warnings");
    expect(wrapper.text()).toContain("skills/broken/SKILL.md");
    expect(wrapper.text()).toContain("review");
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });

  it("reports request errors in operator terms", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          response({ code: "rate_limited", message: "slow down" }, 429),
        ),
    );
    const wrapper = render();
    await previewButton(wrapper).trigger("click");
    await flushPromises();
    expect(wrapper.find('[role="alert"]').text()).toContain(
      "Preview limit reached",
    );
  });

  it("shows the validation detail and hands field problems to the editor", async () => {
    const message =
      'slug "mattpocock/skills" must use only lowercase letters, digits and hyphens';
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValueOnce(
        response(
          {
            code: "invalid_configuration",
            message,
            fields: { slug: message },
          },
          422,
        ),
      ),
    );
    const wrapper = render();
    await previewButton(wrapper).trigger("click");
    await flushPromises();
    const alert = wrapper.find('[role="alert"]').text();
    expect(alert).toContain(message);
    expect(alert).toContain("highlighted field");
    const rejected = wrapper.emitted("rejected");
    expect(rejected).toHaveLength(1);
    expect(rejected![0]![0]).toBeInstanceOf(AdminError);
    expect((rejected![0]![0] as AdminError).fields).toEqual({ slug: message });
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false);
  });

  it("does not promise highlights when the rejection has no field detail", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValueOnce(
        response(
          {
            code: "invalid_configuration",
            message: "Only a registry draft can be previewed.",
          },
          422,
        ),
      ),
    );
    const wrapper = render();
    await previewButton(wrapper).trigger("click");
    await flushPromises();
    const alert = wrapper.find('[role="alert"]').text();
    expect(alert).toContain("Only a registry draft can be previewed.");
    expect(alert).not.toContain("highlighted");
  });
});
