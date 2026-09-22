import { defineComponent, h } from "vue";
import AdminField from "../../app/components/AdminField.vue";
import AdminHint from "../../app/components/AdminHint.vue";
import AdminIdChip from "../../app/components/AdminIdChip.vue";
import AdminStatusBadge from "../../app/components/AdminStatusBadge.vue";
import AdminTime from "../../app/components/AdminTime.vue";

// Nuxt UI primitives rendered as plain elements so admin component tests can
// assert on text and roles without the Nuxt runtime.
export const adminStubs = {
  AdminField,
  AdminHint,
  AdminIdChip,
  AdminStatusBadge,
  AdminTime,
  UButton: defineComponent({
    props: { loading: Boolean, disabled: Boolean },
    setup(props, { attrs, slots }) {
      return () =>
        h(
          "button",
          { ...attrs, disabled: props.disabled || undefined },
          slots.default?.(),
        );
    },
  }),
  UBadge: defineComponent({
    props: { color: String },
    setup(props, { attrs, slots }) {
      return () =>
        h("span", { ...attrs, "data-color": props.color }, slots.default?.());
    },
  }),
  UIcon: defineComponent({
    setup() {
      return () => h("span");
    },
  }),
  UInput: defineComponent({
    props: { modelValue: String },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit }) {
      return () =>
        h("input", {
          ...attrs,
          value: props.modelValue,
          onInput: (event: Event) =>
            emit("update:modelValue", (event.target as HTMLInputElement).value),
        });
    },
  }),
  USelect: defineComponent({
    props: { modelValue: String, items: Array },
    setup(props, { attrs }) {
      return () =>
        h(
          "select",
          { ...attrs, value: props.modelValue },
          (props.items as { label: string; value: string }[] | undefined)?.map(
            (item) => h("option", { value: item.value }, item.label),
          ),
        );
    },
  }),
  UTooltip: defineComponent({
    props: { text: String },
    setup(props, { slots }) {
      return () =>
        h("span", [
          slots.default?.(),
          h("span", { role: "tooltip" }, slots.content?.() ?? props.text),
        ]);
    },
  }),
};
