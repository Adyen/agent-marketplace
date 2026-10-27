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

const installationCountDescription = 'Installations that reported this resource enabled in their latest configuration within the last 30 days'
const installationCountText = computed(() => {
  const count = props.item.installation_count
  if (count === undefined) return undefined
  return `${count} reported ${count === 1 ? 'installation' : 'installations'}`
})
const installationCountLabel = computed(() =>
  installationCountText.value === undefined ? undefined : `${installationCountText.value}. ${installationCountDescription}`
)
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
          <span class="resource-row__kind shrink-0 text-muted">{{ kindLabel(item.kind) }}</span>
          <span class="resource-row__metadata-separator text-dimmed" aria-hidden="true">·</span>
          <h2 class="resource-row__coordinate relative z-10 min-w-0 truncate">
            <NuxtLink v-if="browserUrl" :to="browserUrl" :aria-label="`${registryLabel(item)}/${item.name}`" class="resource-coordinate-link relative cursor-pointer rounded text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--am-color-action-focus-ring)">
              <span class="resource-row__registry text-muted">{{ registryLabel(item) }}</span><span class="resource-coordinate__separator am-brand-slash am-brand-slash--separator">/</span><span class="resource-row__name text-highlighted">{{ item.name }}</span>
            </NuxtLink>
            <span v-else :aria-label="`${registryLabel(item)}/${item.name}`"><span class="resource-row__registry text-muted">{{ registryLabel(item) }}</span><span class="resource-coordinate__separator am-brand-slash am-brand-slash--separator">/</span><span class="resource-row__name text-highlighted">{{ item.name }}</span></span>
          </h2>
        </div>
        <p v-if="item.description" class="resource-row__description truncate text-toned">{{ item.description }}</p>
        <p v-else class="resource-row__description truncate italic text-dimmed">No description provided.</p>
      </div>

      <div class="resource-row__utility">
        <div class="resource-row__action">
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
              class="resource-row__availability inline-flex min-h-11 cursor-help items-center gap-1.5 whitespace-nowrap rounded focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--am-color-action-focus-ring)"
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
        <p
          v-if="installationCountText !== undefined"
          :aria-label="installationCountLabel"
          :title="installationCountDescription"
          class="resource-row__installations"
        >
          {{ installationCountText }}
        </p>
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

<style scoped>
.resource-coordinate-link {
  display: inline-block;
  max-width: 100%;
}

.resource-row__kind,
.resource-row__metadata-separator,
.resource-row__registry {
  font: var(--am-type-resource-registry);
}

.resource-row__coordinate {
  font-family: var(--am-font-sans);
  line-height: 1.5rem;
}

.resource-row__name {
  font: var(--am-type-resource-name);
}

.resource-row__description {
  margin-top: 0.25rem;
  font: var(--am-type-resource-description);
}

.resource-row__availability,
.resource-row__installations {
  font: var(--am-type-metadata);
}

.resource-coordinate-link::after {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 1px;
  background: var(--am-color-action-primary);
  content: "";
  opacity: 0;
  pointer-events: none;
  transition: opacity 150ms;
}

.resource-coordinate-link:hover::after,
.resource-coordinate-link:focus-visible::after {
  opacity: 1;
}

.resource-row__utility {
  position: relative;
  z-index: 10;
  display: flex;
  grid-column: 1 / -1;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem 0.75rem;
  min-width: 0;
  margin-top: 0.75rem;
}

.resource-row__action {
  display: flex;
  min-height: var(--am-hit-target);
  align-items: center;
  justify-content: flex-end;
  margin-left: auto;
}

.resource-row__installations {
  order: -1;
  overflow: hidden;
  color: var(--am-color-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (min-width: 40rem) {
  .resource-row__utility {
    display: grid;
    grid-row: 1;
    grid-column: 3;
    grid-template-rows: var(--am-hit-target) 1.25rem;
    align-content: center;
    justify-items: end;
    gap: 0.375rem;
    min-width: 9rem;
    min-height: calc(var(--am-hit-target) + 1.625rem);
    margin-top: 0;
  }

  .resource-row__action {
    margin-left: 0;
  }

  .resource-row__installations {
    order: initial;
    text-align: right;
  }
}

@media (prefers-reduced-motion: reduce) {
  .resource-coordinate-link::after {
    transition: none;
  }
}
</style>
