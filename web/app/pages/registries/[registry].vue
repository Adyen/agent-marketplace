<script setup lang="ts">
import { routeParam } from '~/lib/routes'

const route = useRoute()
const registry = routeParam(route.params.registry, 'registry')
const historyPath = `/registries/${encodeURIComponent(registry)}/history`

if (route.path !== historyPath) {
  await navigateTo({ path: '/', query: { registry } }, { redirectCode: 302 })
}
</script>

<template>
  <NuxtPage v-if="route.path === historyPath" />
</template>
