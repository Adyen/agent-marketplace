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
  <header class="border-b border-default bg-default/90 backdrop-blur">
    <div class="mx-auto flex max-w-5xl items-center gap-3 px-4 py-4 sm:px-6 lg:px-8">
      <NuxtLink to="/" class="rounded-lg text-sm font-semibold text-highlighted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">Agent Marketplace</NuxtLink>
      <template v-for="breadcrumb in breadcrumbs" :key="`${breadcrumb.label}-${breadcrumb.to ?? 'current'}`">
        <span class="text-muted" aria-hidden="true">/</span>
        <NuxtLink v-if="breadcrumb.to" :to="breadcrumb.to" class="truncate text-sm text-muted underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{{ breadcrumb.label }}</NuxtLink>
        <span v-else class="truncate text-sm text-muted" aria-current="page">{{ breadcrumb.label }}</span>
      </template>
    </div>
  </header>
</template>
