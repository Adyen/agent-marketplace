import { defineComponent, h, nextTick } from "vue";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import SearchKindFilter from "../../app/components/SearchKindFilter.vue";

const USelect = defineComponent({
  name: "USelect",
  props: { modelValue: String, items: Array, placeholder: String },
  emits: ["update:modelValue"],
  setup:
    (_, { attrs }) =>
    () =>
      h("button", attrs),
});

describe("SearchKindFilter", () => {
  it("uses the all-kinds copy as a placeholder instead of a selectable value", async () => {
    const wrapper = mount(SearchKindFilter, {
      props: { modelValue: "", kinds: ["skill", "command"] },
      global: { stubs: { USelect } },
    });
    const select = wrapper.getComponent(USelect);

    expect(select.props("modelValue")).toBeUndefined();
    expect(select.props("placeholder")).toBe("All resource kinds");
    expect(select.props("items")).toEqual([
      { label: "Skill", value: "skill" },
      { label: "Command", value: "command" },
    ]);
    expect(select.props("items")).not.toContainEqual(
      expect.objectContaining({ value: "" }),
    );

    select.vm.$emit("update:modelValue", "command");
    await nextTick();
    select.vm.$emit("update:modelValue", undefined);
    await nextTick();
    expect(wrapper.emitted("update:modelValue")).toEqual([["command"], [""]]);
  });
});
