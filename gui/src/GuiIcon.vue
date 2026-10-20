<script lang="ts">
const circle = (cx: number, cy: number, r: number) =>
  `M${cx - r} ${cy}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`

export const iconPaths = {
  sidebar: ['M3 4.5a1.5 1.5 0 0 1 1.5-1.5h7a1.5 1.5 0 0 1 1.5 1.5v7a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 3 11.5z', 'M6.5 3v10'],
  inspector: ['M3 4.5a1.5 1.5 0 0 1 1.5-1.5h7a1.5 1.5 0 0 1 1.5 1.5v7a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 3 11.5z', 'M9.5 3v10'],
  search: [circle(7, 7, 4.5), 'M10.5 10.5l3 3'],
  refresh: ['M13 8a5 5 0 1 1-1.5-3.5', 'M13 2.5V5h-2.5'],
  sync: ['M2.5 8a5.5 5.5 0 0 1 9.5-3.8l1.5 1.3', 'M13.5 2.5v3h-3', 'M13.5 8a5.5 5.5 0 0 1-9.5 3.8l-1.5-1.3', 'M2.5 13.5v-3h3'],
  update: ['M8 13V3', 'M4 7l4-4 4 4'],
  chevronDown: ['M4 6l4 4 4-4'],
  chevronRight: ['M6 4l4 4-4 4'],
  plus: ['M8 3v10', 'M3 8h10'],
  minus: ['M3 8h10'],
  folder: ['M2.5 4.5a1 1 0 0 1 1-1h2.7l1.3 1.5h5a1 1 0 0 1 1 1v5.5a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1z'],
  branch: [circle(4, 3.5, 1.5), circle(4, 12.5, 1.5), circle(12, 4.5, 1.5), 'M4 5v6', 'M12 6c0 3-8 2-8 5'],
  globe: [circle(8, 8, 6), 'M2 8h12', 'M8 2c-1.8 2-2.7 4-2.7 6s.9 4 2.7 6c1.8-2 2.7-4 2.7-6S9.8 4 8 2z'],
  gear: [
    'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z',
    circle(12, 12, 3),
  ],
  close: ['M4 4l8 8M12 4l-8 8'],
  check: ['M3.5 8.5l3 3 6-7'],
  warning: ['M8 2.5L14 13H2z', 'M8 6.5v3', 'M8 11.3v.2'],
  info: [circle(8, 8, 6), 'M8 7.2v3.6', 'M8 5v.2'],
  error: [circle(8, 8, 6), 'M5.8 5.8l4.4 4.4M10.2 5.8l-4.4 4.4'],
  file: ['M4 1.8h5l3 3v9.4H4z', 'M9 1.8v3h3'],
  external: ['M9 2.5h4.5V7', 'M13.5 2.5l-6 6', 'M11.5 9.5v3.5h-9v-9h3.5'],
  quit: ['M9.5 2.5h-6a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h6', 'M7 8h7M11 5.5L13.5 8 11 10.5'],
  sparkle: ['M8 2v3M8 11v3M2 8h3M11 8h3'],
  view: ['M3 4h10', 'M3 8h10', 'M3 12h6'],
} satisfies Record<string, string[]>

export type IconName = keyof typeof iconPaths
</script>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ name: IconName }>()

const paths = computed(() => iconPaths[props.name])
const onTwentyFourGrid = computed(() => props.name === 'gear')
</script>

<template>
  <svg
    class="gui-icon"
    viewBox="0 0 16 16"
    width="16"
    height="16"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <g v-if="onTwentyFourGrid" transform="scale(0.6667)" stroke-width="2.25">
      <path v-for="(d, index) in paths" :key="index" :d="d" />
    </g>
    <template v-else>
      <path v-for="(d, index) in paths" :key="index" :d="d" />
    </template>
  </svg>
</template>
