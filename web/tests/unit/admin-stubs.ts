import { defineComponent, h } from 'vue'
import AdminHint from '../../app/components/AdminHint.vue'
import AdminIdChip from '../../app/components/AdminIdChip.vue'
import AdminStatusBadge from '../../app/components/AdminStatusBadge.vue'
import AdminTime from '../../app/components/AdminTime.vue'

// Nuxt UI primitives rendered as plain elements so admin component tests can
// assert on text and roles without the Nuxt runtime.
export const adminStubs = {
  AdminHint,
  AdminIdChip,
  AdminStatusBadge,
  AdminTime,
  UButton: defineComponent({ props: { loading: Boolean, disabled: Boolean }, setup(props, { attrs, slots }) { return () => h('button', { ...attrs, disabled: props.disabled || undefined }, slots.default?.()) } }),
  UBadge: defineComponent({ props: { color: String }, setup(props, { attrs, slots }) { return () => h('span', { ...attrs, 'data-color': props.color }, slots.default?.()) } }),
  UIcon: defineComponent({ setup() { return () => h('span') } }),
  UTooltip: defineComponent({ props: { text: String }, setup(props, { slots }) { return () => h('span', [slots.default?.(), h('span', { role: 'tooltip' }, slots.content?.() ?? props.text)]) } }),
}
