<script setup lang="ts">
import { computed } from 'vue'
import { installCommand, kindLabel, listAvailabilityCopy, resourceLatestLocator, resourceLocator, shortDigest, sourceLabel } from '~/lib/presentation'
import { useCopyText } from '~/composables/useCopyText'
import type { ResourceListItem } from '~/types/marketplace'

type Props = {
  item: ResourceListItem
  canonicalUrl: string
}

const props = defineProps<Props>()
const { copied, copyError, copy } = useCopyText()

const browserUrl = computed(() => resourceLocator(props.item, props.canonicalUrl))
const latestBrowserUrl = computed(() => resourceLatestLocator(props.item, props.canonicalUrl))
const command = computed(() => installCommand(props.item, props.canonicalUrl))
const availability = computed(() => listAvailabilityCopy(props.item))
// An unreported status is unknown, not installable-or-not, so it never gates.
const installable = computed(() => availability.value?.installable !== false)
const urlAvailable = computed(() => Boolean(browserUrl.value && command.value))
const commandAvailable = computed(() => urlAvailable.value && installable.value)

const dotClass = computed(() => {
  switch (availability.value?.color) {
    case 'success': return 'bg-success'
    case 'error': return 'bg-error'
    default: return 'bg-warning'
  }
})

// The row shows only the verdict; the results header explains what it means
// once, instead of repeating a sentence on every entry. A disabled control
// still needs its reason, so the reason rides on the label.
const commandLabel = computed(() => {
  if (!urlAvailable.value) return `Install command unavailable for ${props.item.name}: this Marketplace returned an invalid route value`
  if (!installable.value) return `${props.item.name} cannot be installed: ${availability.value?.label}`
  return `Copy pinned install command for ${props.item.name}`
})
</script>

<template>
  <article class="group flex gap-3 border-b border-default px-3 py-2.5 transition-colors hover:bg-muted/60 motion-reduce:transition-none sm:px-4">
    <!-- Content addressing made visible: same content, same mark. -->
    <ResourceFingerprint :digest="item.content_digest" :size="26" class="mt-0.5" />

    <div class="min-w-0 flex-1">
      <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <span class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">{{ kindLabel(item.kind) }}</span>

        <!-- The coordinate is the address: it is the URL and the CLI argument,
             so source is part of the identifier rather than a lesser caption. -->
        <h2 class="min-w-0 font-mono text-sm">
          <NuxtLink
            v-if="latestBrowserUrl"
            :to="latestBrowserUrl"
            class="rounded underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            <span class="text-muted">{{ sourceLabel(item) }}/</span><span class="font-medium text-highlighted">{{ item.name }}</span>
          </NuxtLink>
          <span v-else><span class="text-muted">{{ sourceLabel(item) }}/</span><span class="font-medium text-highlighted">{{ item.name }}</span></span>
        </h2>

        <span v-if="availability" class="ml-auto inline-flex shrink-0 items-center gap-1.5 text-xs">
          <span :class="dotClass" class="size-1.5 rounded-full" aria-hidden="true" />
          <span class="text-muted">{{ availability.label }}</span>
        </span>
      </div>

      <p v-if="item.description" class="mt-0.5 line-clamp-2 max-w-3xl text-sm leading-snug text-toned">{{ item.description }}</p>
      <p v-else class="mt-0.5 text-sm italic leading-snug text-dimmed">This resource kind carries no description.</p>

      <div class="mt-1 flex flex-wrap items-center gap-x-3">
        <span class="font-mono text-xs text-muted" :title="item.content_digest">{{ shortDigest(item.content_digest) }}</span>
        <UButton
          size="xs"
          color="neutral"
          variant="link"
          class="px-0 underline-offset-4 hover:underline"
          :aria-label="commandLabel"
          :title="commandLabel"
          :disabled="!commandAvailable"
          @click="copy(command, 'command')"
        >
          {{ copied === 'command' ? 'Copied pin' : 'Copy pin' }}
        </UButton>
        <UButton
          size="xs"
          color="neutral"
          variant="link"
          class="px-0 underline-offset-4 hover:underline"
          :aria-label="`Copy canonical browser URL for ${item.name}`"
          :disabled="!urlAvailable"
          @click="copy(browserUrl, 'url')"
        >
          {{ copied === 'url' ? 'Copied URL' : 'Copy URL' }}
        </UButton>
        <span v-if="copyError" role="alert" class="text-xs text-error">Clipboard access failed. Copy the digest manually.</span>
      </div>

      <p class="sr-only" aria-live="polite">
        {{ copied === 'url' ? 'Canonical browser URL copied.' : copied === 'command' ? 'Pinned install command copied.' : '' }}
      </p>
    </div>
  </article>
</template>
