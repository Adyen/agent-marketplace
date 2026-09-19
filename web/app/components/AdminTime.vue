<script setup lang="ts">
import { computed } from 'vue'
import { absoluteAdminTime, relativeAdminTime } from '~/lib/admin-presentation'

const props = defineProps<{ value: string | undefined }>()
const relative = computed(() => relativeAdminTime(props.value))
const absolute = computed(() => absoluteAdminTime(props.value))
</script>

<template>
  <time :datetime="value" :title="absolute" class="whitespace-nowrap">{{ relative }}<span class="sr-only"> ({{ absolute }})</span></time>
</template>
