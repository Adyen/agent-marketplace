import { describe, expect, it, vi } from "vitest";
import { ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import AdminIdChip from "../../app/components/AdminIdChip.vue";
import { adminStubs } from "./admin-stubs";

const copy = vi.fn();
const copied = ref<string | null>(null);
// useCopyText depends on Nuxt's client flag, so the composable is replaced here.
vi.mock("../../app/composables/useCopyText", () => ({
  useCopyText: () => ({
    copied,
    copyError: ref(null),
    copy: (value: string, target: string) => {
      copy(value, target);
      copied.value = target;
    },
  }),
}));

describe("AdminIdChip", () => {
  it("shortens UUIDs for display but copies and titles the full value", async () => {
    const id = "0f1fa441-6015-4f7d-b773-5eeaf6b7f5c0";
    const wrapper = mount(AdminIdChip, {
      props: { label: "Job", value: id },
      global: { stubs: adminStubs },
    });
    expect(wrapper.find("code").text()).toBe("0f1fa441");
    expect(wrapper.find("code").attributes("title")).toBe(id);
    expect(wrapper.find("button").attributes("aria-label")).toBe(
      `Copy Job ID ${id}`,
    );
    await wrapper.find("button").trigger("click");
    await flushPromises();
    expect(copy).toHaveBeenCalledWith(id, "id");
    expect(wrapper.find("button").attributes("aria-label")).toBe(
      "Job ID copied",
    );
  });

  it("renders nothing without a value", () => {
    const wrapper = mount(AdminIdChip, {
      props: { label: "Parent", value: undefined },
      global: { stubs: adminStubs },
    });
    expect(wrapper.text()).toBe("");
  });
});
