<script setup lang="ts">
import { computed } from 'vue'
import { installCommand, kindIcon, kindLabel, resourceLatestLocator, resourceLocator, sourceLabel } from '~/lib/presentation'
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
const actionsAvailable = computed(() => Boolean(browserUrl.value && command.value))
</script>

<template>
  <article class="group flex min-h-full flex-col rounded-2xl border border-default bg-default p-5 shadow-sm transition hover:-translate-y-0.5 hover:shadow-md motion-reduce:transition-none">
    <div class="flex items-start justify-between gap-4">
      <div class="flex min-w-0 items-center gap-3">
        <div class="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary" aria-hidden="true">
          <UIcon :name="kindIcon(item.kind)" class="size-5" />
        </div>
        <div class="min-w-0">
          <p class="truncate text-sm font-medium text-muted">{{ sourceLabel(item) }}</p>
          <h2 class="truncate text-lg font-semibold text-highlighted" :title="item.name">
            <NuxtLink v-if="latestBrowserUrl" :to="latestBrowserUrl" class="rounded focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{{ item.name }}</NuxtLink>
            <span v-else>{{ item.name }}</span>
          </h2>
        </div>
      </div>
      <UBadge color="neutral" variant="subtle" class="shrink-0">{{ kindLabel(item.kind) }}</UBadge>
    </div>

    <p class="mt-5 line-clamp-3 flex-1 text-sm leading-6 text-muted">
      {{ item.description || 'No description provided for this resource.' }}
    </p>

    <div class="mt-5 border-t border-default pt-4">
      <p class="text-xs text-muted">Revision <span class="font-mono text-highlighted">{{ item.revision }}</span></p>
      <p class="mt-1 truncate font-mono text-xs text-dimmed" :title="item.content_digest">{{ item.content_digest }}</p>
      <div class="mt-3 flex flex-wrap gap-2">
        <UButton
          size="sm"
          color="primary"
          variant="soft"
          icon="i-lucide-link"
          :aria-label="`Copy canonical browser URL for ${item.name}`"
          :disabled="!actionsAvailable"
          @click="copy(browserUrl, 'url')"
        >
          {{ copied === 'url' ? 'Copied URL' : 'Copy URL' }}
        </UButton>
        <UButton
          size="sm"
          color="neutral"
          variant="ghost"
          icon="i-lucide-terminal"
          :aria-label="`Copy CLI install command for ${item.name}`"
          :disabled="!actionsAvailable"
          @click="copy(command, 'command')"
        >
          {{ copied === 'command' ? 'Copied command' : 'Copy CLI command' }}
        </UButton>
      </div>
      <p v-if="!actionsAvailable" role="status" class="mt-3 text-sm text-warning">Actions unavailable: the Marketplace returned an invalid route value.</p>
      <p v-else-if="copyError" role="alert" class="mt-3 text-sm text-error">Clipboard access failed. Copy the displayed value manually.</p>
      <p class="sr-only" aria-live="polite">
        {{ copied === 'url' ? 'Canonical browser URL copied.' : copied === 'command' ? 'CLI install command copied.' : '' }}
      </p>
    </div>
  </article>
</template>
