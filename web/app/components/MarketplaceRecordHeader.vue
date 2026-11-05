<script setup lang="ts">
type Breadcrumb = {
  label: string
  to?: string
}

defineProps<{
  breadcrumbs: readonly Breadcrumb[]
}>()
</script>

<template>
  <nav aria-label="Breadcrumb" class="mb-8 flex min-w-0 items-center gap-2 overflow-hidden font-mono text-xs">
    <template v-for="(breadcrumb, index) in breadcrumbs" :key="`${breadcrumb.label}-${breadcrumb.to ?? 'current'}`">
      <NuxtLink v-if="breadcrumb.to" :to="breadcrumb.to" class="truncate text-muted underline-offset-4 hover:text-highlighted hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{{ breadcrumb.label }}</NuxtLink>
      <span v-else class="truncate text-muted" aria-current="page">{{ breadcrumb.label }}</span>
      <span v-if="index < breadcrumbs.length - 1" class="text-muted" aria-hidden="true">/</span>
    </template>
  </nav>
</template>
