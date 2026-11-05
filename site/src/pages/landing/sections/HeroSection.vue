<script setup lang="ts">
import { ref } from 'vue'

import {
  GITHUB_REPOSITORY_URL,
  INSTALL_COMMAND,
} from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import CommandBar from '../components/CommandBar.vue'
import LandingTabList from '../components/LandingTabList.vue'
import TuiPreview from '../components/TuiPreview.vue'

const previewTabs = [
  { id: 'terminal', label: 'Terminal' },
  { id: 'web', label: 'Web' },
] as const
const activePreview = ref<'terminal' | 'web'>('terminal')
const captions = {
  terminal: 'Select resources, review their targets, then sync.',
  web: 'Browse the catalog, review resources, and copy install commands.',
}
</script>

<template>
  <header class="hero">
    <div class="am-wrap">
      <div class="grid">
        <div class="copy">
          <h1 class="am-text-trim">
            Manage AI agent tooling once<span>.</span>
            <span class="h1-sub">
              Sync it everywhere you work
            </span>
          </h1>
          <p class="lede">
              Manage skills, commands, hooks, and MCP servers from one config, then
              sync them to Claude, Cursor, Codex, and more.
          </p>

          <CommandBar :command="INSTALL_COMMAND" class="install" />

          <div class="actions">
            <ActionLink
              :href="GITHUB_REPOSITORY_URL"
              label="View on GitHub"
              variant="outline"
              icon="external"
              target="_blank"
              rel="noreferrer"
            />
          </div>
        </div>

        <div class="product-stage">
          <LandingTabList
            v-model="activePreview"
            class="hero-tabs"
            id-prefix="hero"
            label="Hero preview"
            :tabs="previewTabs"
          />
          <div
            id="hero-panel-terminal"
            v-show="activePreview === 'terminal'"
            role="tabpanel"
            aria-labelledby="hero-tab-terminal"
            tabindex="0"
          >
            <TuiPreview class="preview" />
          </div>
          <div
            id="hero-panel-web"
            v-show="activePreview === 'web'"
            role="tabpanel"
            aria-labelledby="hero-tab-web"
            tabindex="0"
          >
            <div class="web-preview-placeholder" role="group" aria-label="Marketplace catalog preview">
              Marketplace catalog preview
            </div>
          </div>
          <p class="preview-caption">
            {{ captions[activePreview] }}
          </p>
        </div>
      </div>
    </div>
  </header>
</template>

<style scoped>
.hero {
  padding-top: var(--lp-edge-top, var(--lp-section-pad));
  padding-bottom: var(--lp-edge-bottom, var(--lp-section-pad));
}

.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--lp-space-default);
  align-items: start;
}

.copy,
.product-stage {
  min-width: 0;
}

.hero-tabs {
  margin-bottom: var(--lp-space-default);
}

.web-preview-placeholder {
  display: grid;
  min-height: 16rem;
  place-items: center;
  border: 1px solid currentColor;
  border-radius: var(--lp-radius-preview);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
}

h1 {
  margin: 0;
  color: var(--lp-text);
  font: var(--lp-font-hero);
  text-wrap: balance;
}

.h1-sub {
  display: block;
  color: var(--lp-text-muted);
}

.lede {
  max-width: var(--lp-measure);
  margin: var(--lp-space-default) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

.install {
  margin-top: var(--lp-space-group);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--lp-space-related);
  margin-top: var(--lp-space-default);
}

.preview-caption {
  max-width: var(--lp-measure);
  margin: var(--lp-space-related) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-caption);
  text-wrap: pretty;
}

@media (prefers-reduced-motion: no-preference) {
  @keyframes arrive {
    from {
      opacity: 0;
      transform: translateY(var(--arrive-y, 9px));
    }

    to {
      opacity: 1;
      transform: none;
    }
  }

  :is(h1, .lede, .install, .actions, .preview, .preview-caption) {
    animation: arrive var(--lp-motion-arrive) var(--lp-ease) backwards;
    animation-delay: calc(var(--lp-motion-stagger) * var(--arrive-step, 0));
  }

  h1 {
    --arrive-y: 3px;
  }

  .lede {
    --arrive-step: 1;
  }

  .install {
    --arrive-step: 2;
    --arrive-y: 12px;
  }

  .actions {
    --arrive-step: 3;
    --arrive-y: 12px;
  }

  .preview {
    --arrive-step: 2;
  }

  .preview-caption {
    --arrive-step: 3;
  }
}

@media (max-width: 959px) {
  .grid {
    grid-template-columns: 1fr;
    row-gap: var(--lp-space-group);
  }
}

@media (max-width: 479px) {
  .actions > :deep(.action-link) {
    flex: 1;
  }
}
</style>
