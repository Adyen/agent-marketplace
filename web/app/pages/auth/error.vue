<script setup lang="ts">
import { resolveAdminAuthError } from '~/lib/admin-auth-error'

const route = useRoute()
const config = useRuntimeConfig()
const presentation = computed(() => resolveAdminAuthError(route.query, config.app.baseURL))
const homeHref = config.app.baseURL

useHead(computed(() => ({
  title: `${presentation.value.title} — Marketplace`,
  meta: [
    { name: 'robots', content: 'noindex, nofollow, noarchive' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})))
</script>

<template>
  <main id="main-content" class="auth-error" tabindex="-1">
    <div class="auth-error-card">
      <span class="auth-error-rail" aria-hidden="true" />
      <div class="auth-error-body">
        <p class="auth-error-kicker">
          Marketplace administration
        </p>
        <h1 class="auth-error-title">{{ presentation.title }}</h1>
        <p class="auth-error-message">{{ presentation.message }}</p>
        <p class="auth-error-code">
          Reason: <code>{{ presentation.code }}</code>
        </p>
        <div class="auth-error-actions">
          <a v-if="presentation.retry" :href="presentation.loginHref" class="auth-error-retry">Try signing in again</a>
          <a :href="homeHref" class="auth-error-return">Return to the Marketplace</a>
        </div>
      </div>
    </div>
  </main>
</template>

<style scoped>
.auth-error {
  min-height: 100vh;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: clamp(2rem, 8vh, 6rem) 1rem 4rem;
  color: var(--ui-text);
}

.auth-error-card {
  display: flex;
  width: 100%;
  max-width: 34rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius);
  background: var(--ui-bg-elevated);
  overflow: hidden;
}

.auth-error-rail {
  flex: 0 0 4px;
  background: var(--ui-error);
}

.auth-error-body {
  padding: clamp(1.5rem, 4vw, 2.5rem);
  overflow-wrap: anywhere;
}

.auth-error-kicker {
  margin: 0 0 0.75rem;
  font-size: 0.75rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ui-text-muted);
}

.auth-error-title {
  margin: 0 0 1rem;
  font-family: Georgia, 'Times New Roman', serif;
  font-size: clamp(1.5rem, 4vw, 1.875rem);
  font-weight: 600;
  line-height: 1.25;
  color: var(--ui-text-highlighted);
}

.auth-error-message {
  margin: 0 0 1.25rem;
  line-height: 1.65;
}

.auth-error-code {
  margin: 0 0 1.75rem;
  font-size: 0.8125rem;
  color: var(--ui-text-muted);
}

.auth-error-code code {
  font-family: var(--font-mono);
}

.auth-error-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.5rem;
  align-items: center;
}

.auth-error-actions a {
  border-radius: var(--ui-radius);
}

.auth-error-actions a:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}

.auth-error-retry {
  display: inline-block;
  padding: 0.5rem 1rem;
  background: var(--ui-primary);
  color: var(--ui-bg);
  font-weight: 550;
  text-decoration: none;
}

.auth-error-retry:hover {
  filter: brightness(1.08);
}

.auth-error-return {
  color: var(--ui-primary);
  font-weight: 550;
}

@media (prefers-reduced-motion: no-preference) {
  .auth-error-retry {
    transition: filter 120ms ease-out;
  }
}
</style>
