<script setup lang="ts">
import { computed } from 'vue'
import CopyButton from '~/components/CopyButton.vue'
import { installCommand, kindLabel, listAvailabilityCopy, registryLabel, resourceRoute } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import type { ResourceListItem } from '~/types/marketplace'

type Props = {
  item: ResourceListItem
  canonicalUrl: string
}

const props = defineProps<Props>()
const { copied, copyError, copy } = useCopyText()

const browserUrl = computed(() => resourceRoute(props.item))
const command = computed(() => installCommand(props.item, props.canonicalUrl))
const availability = computed(() => listAvailabilityCopy(props.item))
const installable = computed(() => availability.value?.installable !== false)
const commandAvailable = computed(() => Boolean(command.value) && installable.value)
const statusTextClass = computed(() => {
  switch (availability.value?.color) {
    case 'success': return 'text-success'
    case 'error': return 'text-error'
    case 'warning': return 'text-warning'
    default: return 'text-muted'
  }
})

const commandLabel = computed(() => {
  if (!command.value) return `Install command unavailable for ${props.item.name}: this Marketplace returned an invalid route value`
  return `Copy install command for ${props.item.name}`
})
</script>

<template>
  <article
    :data-interactive="browserUrl ? 'true' : undefined"
    class="marketplace-resource-row group relative border-b border-default px-3 py-2.5 transition-colors motion-reduce:transition-none sm:px-4 sm:py-3"
  >
    <div class="grid min-w-0 grid-cols-[24px_minmax(0,1fr)] items-center gap-x-4 sm:grid-cols-[24px_minmax(0,1fr)_auto]">
      <ResourceFingerprint :digest="item.content_digest" :size="24" />

      <div class="min-w-0">
        <div class="flex min-w-0 items-baseline gap-2">
          <span class="shrink-0 font-mono text-xs font-medium text-muted">{{ kindLabel(item.kind) }}</span>
          <span class="text-xs text-dimmed" aria-hidden="true">·</span>
          <h2 class="min-w-0 truncate font-mono text-sm font-medium leading-5 tracking-tight">
            <NuxtLink
              v-if="browserUrl"
              :to="browserUrl"
              class="group/resource-link cursor-pointer rounded decoration-primary underline-offset-4 after:absolute after:inset-0 hover:underline focus-visible:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            >
              <span class="text-muted">{{ registryLabel(item) }}/</span><span class="font-semibold text-highlighted">{{ item.name }}</span>
            </NuxtLink>
            <span v-else><span class="text-muted">{{ registryLabel(item) }}/</span><span class="font-semibold text-highlighted">{{ item.name }}</span></span>
          </h2>
        </div>

        <p v-if="item.description" class="mt-1 truncate text-sm font-medium leading-5 text-toned">{{ item.description }}</p>
        <p v-else class="mt-1 truncate text-sm italic leading-5 text-dimmed">No description provided.</p>
      </div>

      <div class="relative z-10 col-start-2 mt-2 flex min-h-16 min-w-20 flex-col items-start justify-center sm:col-start-3 sm:row-start-1 sm:mt-0 sm:min-w-28 sm:items-end">
        <UTooltip
          v-if="availability && !installable"
          :delay-duration="0"
          :content="{ side: 'bottom', sideOffset: 8 }"
          :ui="{ content: 'h-auto max-w-sm rounded bg-inverted px-4 py-3 text-inverted shadow-lg ring-0' }"
        >
          <button
            type="button"
            :aria-label="`${availability.label} for ${item.name}. More information`"
            :class="statusTextClass"
            class="inline-flex min-h-11 cursor-help items-center gap-1.5 whitespace-nowrap rounded text-xs leading-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            <span>{{ availability.label }}</span>
            <UIcon name="i-lucide-info" class="size-4 shrink-0" aria-hidden="true" />
          </button>
          <template #content>
            <p class="whitespace-normal text-sm leading-6">{{ availability.installNotice }}</p>
          </template>
        </UTooltip>
        <CopyButton
          v-if="installable"
          :copied="copied === 'command'"
          :copy-label="commandLabel"
          :copied-label="`Install command copied for ${item.name}`"
          copy-text="Copy install command"
          :disabled="!commandAvailable"
          @click="copy(command, 'command')"
        />
      </div>

      <div v-if="copyError" role="alert" class="relative z-10 col-span-2 mt-1 text-xs text-error sm:col-span-3">
        Clipboard access failed. Open the resource details to select and copy the install command.
      </div>

      <p class="sr-only" aria-live="polite">
        {{ copied === 'command' ? 'Install command copied.' : '' }}
      </p>
    </div>
  </article>
</template>
