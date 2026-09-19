<script setup lang="ts">
import { computed } from 'vue'
import { lifecycleBadge, outcomeBadge } from '~/lib/admin-presentation'

const props = defineProps<{ value: string | undefined, kind?: 'lifecycle' | 'outcome' }>()
const badge = computed(() => (props.kind === 'lifecycle' ? lifecycleBadge : outcomeBadge)(props.value))
</script>

<template>
  <UBadge :color="badge.color" variant="subtle" size="sm" class="shrink-0 whitespace-nowrap">{{ badge.label }}</UBadge>
</template>
