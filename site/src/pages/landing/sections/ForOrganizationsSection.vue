<script setup lang="ts">
import { withBase } from 'vitepress'
import { DOCS_PATH } from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import MarketplaceBrowserFrame from '../components/MarketplaceBrowserFrame.vue'
import SectionShell from '../components/SectionShell.vue'

const deploymentGuidePath = withBase(`${DOCS_PATH}how-to/operator/deploy-a-production-marketplace`)
</script>

<template>
  <SectionShell
    id="organizations"
    class="organizations"
    eyebrow="For organizations"
    title="A marketplace for your team"
  >
    <template #sub>
      Give your team a shared place to discover and install AI agent resources.
    </template>
    <template #action>
      <ActionLink
        :href="deploymentGuidePath"
        label="Deployment guide"
        variant="text"
        icon="arrow"
      />
    </template>

    <div class="benefits">
      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <ellipse cx="12" cy="5" rx="7" ry="3" />
            <path d="M5 5v7c0 1.7 3.1 3 7 3s7-1.3 7-3V5" />
            <path d="M5 12v7c0 1.7 3.1 3 7 3s7-1.3 7-3v-7" />
          </svg>
        </span>
        <div>
          <h3>Your registries, together</h3>
          <p>Bring resources from multiple registries into one place.</p>
        </div>
      </article>

      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <path d="M7 3h7l4 4v14H7z" />
            <path d="M14 3v5h4M10 14h5M10 17h3" />
          </svg>
        </span>
        <div>
          <h3>Publication policies</h3>
          <p>Define allowed licenses before resources are published.</p>
        </div>
      </article>

      <article class="benefit">
        <span class="benefit__icon">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9z" />
            <path d="m4 7.5 8 4.5 8-4.5M12 12v9" />
          </svg>
        </span>
        <div>
          <h3>Your infrastructure</h3>
          <p>Run the Marketplace in the environment your team already operates.</p>
        </div>
      </article>
    </div>

    <div class="marketplace-depictions">
      <MarketplaceBrowserFrame view="catalog" />
      <MarketplaceBrowserFrame view="detail" />
    </div>
  </SectionShell>
</template>

<style scoped>
@media (min-width: 960px) {
  .organizations {
    --lp-section-pad: var(--lp-space-group);

    min-height: calc(100svh - var(--lp-nav-height));
  }

  .organizations :deep(.head) {
    margin-bottom: var(--lp-space-group);
  }
}

.benefits {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  column-gap: var(--lp-gutter);
  margin-bottom: var(--lp-space-default);
}

.benefit {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: var(--lp-space-default);
  padding-left: var(--lp-space-default);
  box-shadow: inset 1px 0 0 var(--lp-outline);
}

.benefit__icon {
  display: block;
  width: var(--lp-space-default);
  height: var(--lp-space-default);
  color: var(--lp-text);
}

.benefit__icon svg {
  width: 100%;
  height: 100%;
  fill: none;
  stroke: currentcolor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.5;
}

.benefit h3 {
  margin: 0;
  color: var(--lp-text);
  font: var(--lp-font-heading);
}

.benefit p {
  max-width: var(--lp-measure);
  margin: var(--lp-space-tight) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

.marketplace-depictions {
  display: grid;
  gap: var(--lp-space-default);
}

@media (max-width: 959px) {
  .benefits {
    grid-template-columns: 1fr;
    row-gap: var(--lp-space-group);
  }

}

@media (min-width: 960px) and (max-height: 800px) {
  .organizations {
    --lp-section-pad: var(--lp-space-related);
  }
}
</style>
