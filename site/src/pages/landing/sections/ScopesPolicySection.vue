<script setup lang="ts">
import { computed, ref } from 'vue'
import CommandBar from '../components/CommandBar.vue'
import SectionShell from '../components/SectionShell.vue'

type ScopeID = 'user' | 'repo' | 'local'

const scopes = [
  {
    id: 'user' as ScopeID,
    title: 'User',
    summary: 'Available to you in every repository on this machine.',
    configPath: '~/.agents/config.yaml',
    reach: 'Every repository',
    versionControl: 'Not committed',
  },
  {
    id: 'repo' as ScopeID,
    title: 'Repository',
    summary: 'Shared with the team and committed to this repository.',
    configPath: '.agents/config.yaml',
    reach: 'This repository',
    versionControl: 'Committed',
    isDefault: true,
  },
  {
    id: 'local' as ScopeID,
    title: 'Local',
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
    eyebrow="Scopes &amp; policy"
    title="Control where tools apply"
    soft
  >
    <template #sub>
      Choose a user, repository, or local scope for each tool.
      Organization-required tooling stays enforced.
    </template>

    <div class="scope-panel">
      <div class="tabs" role="tablist" aria-label="Resource scope">
        <button
          v-for="scope in scopes"
          :key="scope.id"
          :id="'tab-' + scope.id"
          type="button"
          role="tab"
          :aria-selected="activeScopeID === scope.id"
          :aria-controls="'panel-' + scope.id"
          :class="{ active: activeScopeID === scope.id }"
          @click="activeScopeID = scope.id"
        >
          {{ scope.title }}
        </button>
      </div>

      <div
        :id="'panel-' + activeScope.id"
        class="scope-detail"
        role="tabpanel"
        :aria-labelledby="'tab-' + activeScope.id"
      >
        <h3>
          {{ activeScope.title }}
          <span v-if="activeScope.isDefault" class="default-tag">default</span>
        </h3>
        <p>{{ activeScope.summary }}</p>

        <dl class="scope-outcome">
          <div>
            <dt>Config file</dt>
            <dd><code>{{ activeScope.configPath }}</code></dd>
          </div>
          <div>
            <dt>Applies to</dt>
            <dd>{{ activeScope.reach }}</dd>
          </div>
          <div>
            <dt>Version control</dt>
            <dd>{{ activeScope.versionControl }}</dd>
          </div>
        </dl>

        <CommandBar
          :command="installCommand"
          :highlight="`--scope ${activeScope.id}`"
          :copyable="false"
          class="scope-command"
        />
      </div>
    </div>

    <aside class="organization-baseline">
      <div class="policy-copy">
        <span class="policy-label">
          <svg aria-hidden="true" viewBox="0 0 24 24">
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
  </SectionShell>
</template>

<style scoped>
.scope-panel {
  padding: 30px;
  border: 1px solid var(--am-border);
  border-radius: var(--am-radius-lg);
  background: var(--am-surface);
  box-shadow: inset 0 1px rgba(255, 255, 255, 0.025);
}

.tabs {
  display: flex;
  gap: 28px;
  border-bottom: 1px solid var(--am-border);
}

.tabs button {
  padding: 0 0 11px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--vp-c-text-2);
  font-size: 15px;
  font-weight: 550;
  cursor: pointer;
}

.tabs button:hover,
.tabs button.active {
  color: var(--vp-c-text-1);
}

.tabs button.active {
  border-bottom-color: var(--vp-c-brand-1);
}

.tabs button:focus-visible {
  outline: 2px solid var(--vp-c-brand-1);
  outline-offset: 3px;
}

.scope-detail {
  padding-top: 24px;
}

h3 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 7px;
  color: var(--vp-c-text-1);
  font-size: 20px;
  font-weight: 650;
}

.default-tag,
.lock {
  padding: 2px 5px;
  border-radius: 4px;
  font-family: var(--vp-font-family-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.05em;
  line-height: 1;
  text-transform: uppercase;
}

.default-tag {
  border: 1px solid var(--am-border);
  color: var(--vp-c-text-2);
}

.scope-detail > p {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: 15px;
  line-height: 1.55;
}

.scope-outcome {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1px;
  margin: 24px 0 0;
  overflow: hidden;
  border: 1px solid var(--am-code-border);
  border-radius: var(--am-radius);
  background: var(--am-code-border);
}

.scope-outcome div {
  min-width: 0;
  padding: 13px 15px;
  background: var(--am-code-bg);
}

.scope-outcome dt {
  margin-bottom: 5px;
  color: var(--am-code-muted);
  font-family: var(--vp-font-family-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
}

.scope-outcome dd {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--am-code-text);
  font-size: 13px;
  font-weight: 600;
}

.scope-outcome code {
  color: var(--vp-c-brand-1);
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
}

.scope-command {
  --am-technical-font-size: 14px;
  width: 100%;
  margin-top: 20px;
}

.organization-baseline {
  position: relative;
  margin-top: 18px;
  padding: 16px 18px;
  overflow: hidden;
  border: 1px solid var(--am-accent-border);
  border-radius: var(--am-radius-lg);
  background:
    linear-gradient(90deg, var(--am-accent-soft), transparent 55%),
    var(--am-surface);
  box-shadow: inset 0 1px rgba(255, 255, 255, 0.025);
}

.organization-baseline::before {
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--vp-c-brand-1);
  content: "";
}

.policy-copy {
  min-width: 0;
}

.policy-label {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 4px;
  color: var(--vp-c-brand-1);
  font-family: var(--vp-font-family-mono);
  font-size: 10px;
  font-weight: 650;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.policy-label svg {
  width: 13px;
  height: 13px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.8;
}

.policy-copy p {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: 14px;
  line-height: 1.5;
}

@media (max-width: 680px) {
  .scope-panel {
    padding: 22px;
  }

  .scope-outcome {
    grid-template-columns: 1fr;
  }

  .tabs {
    gap: 18px;
  }
}
</style>
