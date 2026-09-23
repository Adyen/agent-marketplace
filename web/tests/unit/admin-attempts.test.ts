import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import AdminAttempts from "../../app/components/AdminAttempts.vue";
import { adminStubs } from "./admin-stubs";

const event = {
  sequence: 9,
  registry_id: "team",
  job_id: "refresh-child",
  parent_job_id: "completed-parent",
  attempt: 1,
  outcome: "failed",
  operation: "reconcile",
  recorded_at: "2026-09-19T12:00:00Z",
  configuration_revision: 2,
  registry_generation: 3,
  policy_revision: 1,
};
const wrappers: ReturnType<typeof mount>[] = [];
function render() {
  const wrapper = mount(AdminAttempts, {
    props: { registryId: "team" },
    global: { stubs: adminStubs },
  });
  wrappers.push(wrapper);
  return wrapper;
}
function response(attempts: unknown[], next_before = 0) {
  return new Response(JSON.stringify({ attempts, next_before }), {
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => {
  wrappers.forEach((wrapper) => wrapper.unmount());
  wrappers.length = 0;
  vi.unstubAllGlobals();
});

describe("registry indexing attempts", () => {
  it("shows failed child attempts separately from the parent and loads older events using the server cursor", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response([event], 9))
      .mockResolvedValueOnce(
        response([{ ...event, sequence: 8, outcome: "started" }]),
      );
    vi.stubGlobal("fetch", fetcher);
    const wrapper = render();
    await flushPromises();
    expect(fetcher.mock.calls[0]?.[0]).toBe(
      "/api/v1/admin/attempts?registry_id=team&limit=25",
    );
    const article = wrapper.find("article");
    expect(article.find('[data-color="error"]').text()).toBe("Failed");
    expect(article.text()).toContain("refresh-child");
    expect(article.text()).toContain("completed-parent");
    expect(article.find("time").attributes("datetime")).toBe(
      "2026-09-19T12:00:00Z",
    );
    expect(
      article.find('button[aria-label^="Copy Job ID refresh-child"]').exists(),
    ).toBe(true);
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Load older attempts")!
      .trigger("click");
    await flushPromises();
    expect(fetcher.mock.calls[1]?.[0]).toBe(
      "/api/v1/admin/attempts?registry_id=team&limit=25&before=9",
    );
    expect(wrapper.findAll("article")).toHaveLength(2);
    expect(wrapper.findAll("article")[1]!.text()).toContain("Started");
    expect(wrapper.text()).not.toContain("Load older attempts");
  });

  it("retains loaded events on errors, allows retry, and replaces them on refresh", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response([event], 9))
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(
        response([
          {
            ...event,
            sequence: 10,
            outcome: "succeeded",
            publication_id: "published-10",
          },
        ]),
      );
    vi.stubGlobal("fetch", fetcher);
    const wrapper = render();
    await flushPromises();
    const refresh = () =>
      wrapper
        .findAll("button")
        .find((button) => button.text() === "Refresh indexing attempts")!
        .trigger("click");
    await refresh();
    await flushPromises();
    expect(wrapper.find('[role="alert"]').exists()).toBe(true);
    expect(wrapper.find("article").text()).toContain("Failed");
    await refresh();
    await flushPromises();
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
    expect(wrapper.findAll("article")).toHaveLength(1);
    expect(wrapper.find("article").text()).toContain("Succeeded");
    expect(wrapper.find("article").text()).toContain("published-10");
    expect(fetcher.mock.calls[2]?.[0]).not.toContain("before=");
  });

  it("does not present a failed initial request as an empty successful history", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("offline")));
    const wrapper = render();
    await flushPromises();
    expect(wrapper.find('[role="alert"]').exists()).toBe(true);
    expect(wrapper.text()).not.toContain("No indexing attempts");
  });
});
