<script setup lang="ts">
export type Crumb = { label: string; to?: string };
defineProps<{ items: Crumb[] }>();
</script>

<template>
  <nav aria-label="Breadcrumb" class="text-sm">
    <ol class="flex flex-wrap items-center gap-1.5">
      <li
        v-for="(item, index) in items"
        :key="`${item.label}-${index}`"
        class="flex items-center gap-1.5"
      >
        <UIcon
          v-if="index"
          name="i-lucide-chevron-right"
          class="size-3.5 text-dimmed"
          aria-hidden="true"
        />
        <NuxtLink
          v-if="item.to && index < items.length - 1"
          :to="item.to"
          class="rounded text-muted hover:text-highlighted hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >{{ item.label }}</NuxtLink
        >
        <span v-else class="font-medium text-highlighted" aria-current="page">{{
          item.label
        }}</span>
      </li>
    </ol>
  </nav>
</template>
