<script setup lang="ts">
import { computed, ref } from 'vue'
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'

type ScopeID = 'user' | 'repo' | 'local'

const scopes = [
  {
    id: 'user' as ScopeID,
    title: 'User',
    context: 'Personal',
    summary: 'Available to you in every repository on this machine.',
    configPath: '~/.agents/config.yaml',
    reach: 'Every repository',
    versionControl: 'Not committed',
  },
  {
    id: 'repo' as ScopeID,
    title: 'Repository',
    context: 'Team shared',
    summary: 'Shared with the team and committed to this repository.',
    configPath: '.agents/config.yaml',
    reach: 'This repository',
    versionControl: 'Committed',
    isDefault: true,
  },
  {
    id: 'local' as ScopeID,
    title: 'Local',
    context: 'Private',
    summary: 'Available only to you in this repository and kept out of Git.',
    configPath: '.agents/config.local.yaml',
    reach: 'This checkout',
    versionControl: 'Gitignored',
  },
]

const activeScopeID = ref<ScopeID>('repo')
const activeScope = computed(
  () => scopes.find((scope) => scope.id === activeScopeID.value) ?? scopes[1],
)
const installCommand = computed(
  () => `agent-marketplace install pdf --scope ${activeScope.value.id}`,
)
</script>

<template>
  <SectionShell
    id="scopes"
    class="scopes-policy"
    eyebrow="Scopes &amp; policy"
    title="Control where tools apply"
  >
    <template #sub>
      Choose who receives a tool and whether its configuration is shared.
    </template>

    <div class="scope-explorer">
      <fieldset class="scope-fieldset">
        <legend class="visually-hidden">Select an installation scope</legend>
        <div class="scope-grid">
          <label v-for="(scope, index) in scopes" :key="scope.id" class="scope-card">
            <input
              v-model="activeScopeID"
              class="scope-radio visually-hidden"
              type="radio"
              name="installation-scope"
              :value="scope.id"
            >
            <span class="scope-card__surface">
              <span class="scope-card__context">
                <span>{{ String(index + 1).padStart(2, '0') }}</span>
                <span aria-hidden="true">/</span>
                {{ scope.context }}
              </span>

              <span class="scope-card__title">
                <span>{{ scope.title }}</span>
                <span v-if="scope.isDefault" class="default-tag">Default</span>
              </span>

              <span class="scope-card__summary">{{ scope.summary }}</span>

              <span class="scope-facts">
                <span class="scope-fact scope-fact--config">
                  <span class="scope-fact__label">Config file</span>
                  <code>{{ scope.configPath }}</code>
                </span>
                <span class="scope-fact">
                  <span class="scope-fact__label">Applies to</span>
                  <span>{{ scope.reach }}</span>
                </span>
                <span class="scope-fact">
                  <span class="scope-fact__label">Version control</span>
                  <span>{{ scope.versionControl }}</span>
                </span>
              </span>
            </span>
          </label>
        </div>
      </fieldset>

      <CommandBar
        :command="installCommand"
        :highlight="`--scope ${activeScope.id}`"
        :copyable="false"
        class="scope-command"
      />

      <aside class="organization-baseline">
        <div class="policy-copy">
          <span class="policy-label">
            <svg class="am-stroke-icon" aria-hidden="true" viewBox="0 0 24 24">
              <path d="M8 10V7a4 4 0 0 1 8 0v3" />
              <rect x="5" y="10" width="14" height="11" rx="2" />
            </svg>
            Organization baseline
          </span>
          <p>
            Required tools are defined centrally and cannot be overridden by user,
            repository, or local configuration.
          </p>
        </div>
      </aside>
    </div>
  </SectionShell>
</template>

<style scoped>
.scopes-policy :deep(.head) {
  margin-bottom: var(--lp-space-default);
}

.scope-fieldset {
  min-width: 0;
  padding: 0;
  margin: 0;
  border: 0;
}

.scope-grid {
  display: grid;
  padding: var(--lp-space-tight);
  overflow: hidden;
  border-radius: var(--lp-radius-preview);
  background: var(--lp-surface);
  box-shadow: var(--lp-outline-shadow);
  gap: var(--lp-space-tight);
  grid-auto-rows: 1fr;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.scope-card {
  position: relative;
  min-width: 0;
  cursor: pointer;
}

.scope-card__surface {
  position: relative;
  display: flex;
  height: 100%;
  min-height: 19.5rem;
  flex-direction: column;
  padding: var(--lp-space-default);
  overflow: hidden;
  border-radius: calc(var(--lp-radius-preview) - var(--lp-space-tight));
  background: var(--lp-canvas);
  color: var(--lp-text);
  transition:
    background-color var(--lp-motion-feedback) var(--lp-ease),
    box-shadow var(--lp-motion-feedback) var(--lp-ease);
}

.scope-card:hover .scope-card__surface {
  background: var(--lp-surface);
}

.scope-radio:checked + .scope-card__surface {
  background: var(--lp-fill);
  box-shadow: inset 0 0 0 1px var(--lp-outline);
}

.scope-radio:focus-visible + .scope-card__surface {
  z-index: 1;
  border-radius: var(--lp-radius-control);
  outline: var(--lp-focus-width) solid var(--lp-focus);
  outline-offset: calc(-1 * var(--lp-focus-width));
}

.scope-card__context {
  display: flex;
  align-items: center;
  gap: var(--lp-space-tight);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.scope-card__context > span:first-child {
  color: var(--lp-text);
}

.scope-card__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--lp-space-related);
  margin-top: var(--lp-space-default);
  font: var(--lp-font-heading);
}

.default-tag {
  display: inline-flex;
  height: var(--lp-h-depiction);
  align-items: center;
  padding: 0 var(--lp-space-related);
  border-radius: var(--lp-radius-pill);
  background: var(--lp-fill);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.scope-card__summary {
  min-height: 2.75rem;
  margin-top: var(--lp-space-related);
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

.scope-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--lp-space-default) var(--lp-space-related);
  margin-top: var(--lp-space-default);
}

.scope-fact {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: var(--lp-space-tight);
  color: var(--lp-text);
  font: var(--lp-font-depiction);
}

.scope-fact--config {
  grid-column: 1 / -1;
}

.scope-fact__label {
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.scope-fact code {
  overflow-wrap: anywhere;
  color: var(--lp-text);
  font: var(--lp-font-label);
}

.scope-command {
  margin-top: var(--lp-space-default);
}

.organization-baseline {
  margin-top: var(--lp-space-default);
}

.policy-copy {
  min-width: 0;
}

.policy-label {
  display: flex;
  align-items: center;
  gap: var(--lp-space-tight);
  color: var(--lp-text);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.policy-copy p {
  max-width: var(--lp-measure);
  margin: var(--lp-space-related) 0 0;
  color: var(--lp-text-2);
  font: var(--lp-font-body);
  text-wrap: pretty;
}

@media (max-width: 959px) {
  .scope-card__surface {
    min-height: 21rem;
  }
}

@media (max-width: 767px) {
  .scope-grid {
    grid-template-columns: 1fr;
  }

  .scope-card__surface {
    min-height: 18.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .scope-card__surface {
    transition: none;
  }
}
</style>
