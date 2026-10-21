<script setup lang="ts">
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'

const scopes = [
  {
    id: 'user',
    title: 'User',
    context: 'Personal',
    summary: 'Available to you in every repository on this machine.',
    configPath: '~/.agents/config.yaml',
    reach: 'Every repository',
    versionControl: 'Not committed',
  },
  {
    id: 'repo',
    title: 'Repository',
    context: 'Team shared',
    summary: 'Shared with the team and committed to this repository.',
    configPath: '.agents/config.yaml',
    reach: 'This repository',
    versionControl: 'Committed',
    isDefault: true,
  },
  {
    id: 'local',
    title: 'Local',
    context: 'Private',
    summary: 'Available only to you in this repository and kept out of Git.',
    configPath: '.agents/config.local.yaml',
    reach: 'This checkout',
    versionControl: 'Gitignored',
  },
]
</script>

<template>
  <SectionShell
    id="scopes"
    class="scopes-policy"
    eyebrow="Scopes &amp; policy"
    title="Control where tools apply"
  >
    <template #sub>
      Compare who receives a tool and whether its configuration is shared.
    </template>

    <div class="scope-explorer">
      <div class="scope-grid">
        <article v-for="(scope, index) in scopes" :key="scope.id" class="scope-card">
          <div class="scope-card__context">
            <span class="scope-card__index">
              <span>{{ String(index + 1).padStart(2, '0') }}</span>
              <span aria-hidden="true">/</span>
              {{ scope.context }}
            </span>
            <code>--scope {{ scope.id }}</code>
          </div>

          <h3 class="scope-card__title">
            <span>{{ scope.title }}</span>
            <span v-if="scope.isDefault" class="default-tag">Default</span>
          </h3>

          <p class="scope-card__summary">{{ scope.summary }}</p>

          <div class="scope-facts">
            <div class="scope-fact scope-fact--config">
              <span class="scope-fact__label">Config file</span>
              <code>{{ scope.configPath }}</code>
            </div>
            <div class="scope-fact">
              <span class="scope-fact__label">Applies to</span>
              <span>{{ scope.reach }}</span>
            </div>
            <div class="scope-fact">
              <span class="scope-fact__label">Version control</span>
              <span>{{ scope.versionControl }}</span>
            </div>
          </div>
        </article>
      </div>

      <div class="scope-command">
        <p class="scope-command__label">
          <span>Example</span>
          <span>Repository scope</span>
        </p>
        <CommandBar
          command="agent-marketplace install pdf --scope repo"
          highlight="--scope repo"
        />
      </div>

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

.scope-grid {
  display: grid;
  overflow: hidden;
  border-radius: var(--lp-radius-preview);
  background: var(--lp-canvas);
  box-shadow: var(--lp-outline-shadow);
  grid-auto-rows: 1fr;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.scope-card {
  position: relative;
  display: flex;
  min-height: 19.5rem;
  min-width: 0;
  flex-direction: column;
  padding: var(--lp-space-default);
  color: var(--lp-text);
}

.scope-card + .scope-card::before {
  position: absolute;
  top: var(--lp-space-default);
  bottom: var(--lp-space-default);
  left: 0;
  width: 1px;
  background: var(--lp-outline);
  content: "";
}

.scope-card__context {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--lp-space-related);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
}

.scope-card__index {
  display: flex;
  align-items: center;
  gap: var(--lp-space-tight);
}

.scope-card__index > span:first-child {
  color: var(--lp-text);
}

.scope-card__context code {
  color: var(--lp-text);
  font: inherit;
  text-transform: none;
}

.scope-card__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--lp-space-related);
  margin: var(--lp-space-default) 0 0;
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
  margin: var(--lp-space-related) 0 0;
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

.scope-command__label {
  display: flex;
  justify-content: space-between;
  margin: 0 0 var(--lp-space-tight);
  color: var(--lp-text-2);
  font: var(--lp-font-label);
  text-transform: uppercase;
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

@media (max-width: 767px) {
  .scope-grid {
    grid-template-columns: 1fr;
  }

  .scope-card {
    min-height: auto;
  }

  .scope-card + .scope-card::before {
    top: 0;
    right: var(--lp-space-default);
    bottom: auto;
    left: var(--lp-space-default);
    width: auto;
    height: 1px;
  }
}
</style>
