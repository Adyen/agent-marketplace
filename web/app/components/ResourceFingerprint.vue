<script setup lang="ts">
import { computed } from 'vue'
import { FINGERPRINT_SIDE, fingerprintCells, shortDigest } from '~/lib/presentation'

type Props = {
  digest: string
  /** Rendered size in px. The mark is a square. */
  size?: number
}

const props = withDefaults(defineProps<Props>(), { size: 26 })

// Weights arrive already quantized, and are used as opacity directly. The mark
// is ink-only so it never competes with a verdict colour.
const cells = computed(() => fingerprintCells(props.digest))
</script>

<template>
  <svg
    v-if="cells.length"
    :width="size"
    :height="size"
    :viewBox="`0 0 ${FINGERPRINT_SIDE} ${FINGERPRINT_SIDE}`"
    class="shrink-0 text-highlighted"
    role="img"
    :aria-label="`Content fingerprint for ${shortDigest(digest)}`"
    shape-rendering="crispEdges"
  >
    <rect
      v-for="(weight, index) in cells"
      :key="index"
      :x="index % FINGERPRINT_SIDE"
      :y="Math.floor(index / FINGERPRINT_SIDE)"
      width="1"
      height="1"
      fill="currentColor"
      :opacity="weight"
    />
  </svg>
  <!-- No digest, no mark: an invented one would imply an identity we do not have. -->
  <span v-else :style="{ width: `${size}px`, height: `${size}px` }" class="shrink-0" aria-hidden="true" />
</template>
