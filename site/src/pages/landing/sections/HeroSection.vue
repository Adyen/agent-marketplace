<script setup lang="ts">
import {
  GITHUB_REPOSITORY_URL,
  INSTALL_COMMAND,
} from '../../../shared/site'
import ActionLink from '../components/ActionLink.vue'
import CommandBar from '../components/CommandBar.vue'
import TuiPreview from '../components/TuiPreview.vue'
</script>

<template>
  <header class="hero">
    <div class="am-wrap">
      <div class="grid">
        <div class="copy">
          <h1>
            Manage AI agent tooling once
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
              class="hero-action"
              :href="GITHUB_REPOSITORY_URL"
              variant="outline"
              target="_blank"
              rel="noreferrer"
            >
              View on GitHub
              <span aria-hidden="true">↗</span>
            </ActionLink>
          </div>
        </div>

        <div class="product-stage">
        <TuiPreview />
          <p class="preview-caption">
            Select resources, review their targets, then sync.
          </p>
        </div>
      </div>

    </div>
  </header>
</template>

<style scoped>
.hero {
  position: relative;
  z-index: 1;
  overflow: hidden;
  padding: 120px 0 80px;
  background:
    radial-gradient(circle at 76% 44%, var(--am-hero-glow), transparent 31rem),
    linear-gradient(var(--am-hero-grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--am-hero-grid) 1px, transparent 1px),
    var(--am-hero-bg);
  background-size: auto, 44px 44px, 44px 44px, auto;
  color: var(--am-hero-text);
}

.hero::after {
  position: absolute;
  right: -14rem;
  bottom: -22rem;
  width: 44rem;
  height: 44rem;
  border: 1px solid var(--am-hero-ring);
  border-radius: 50%;
  background: radial-gradient(circle, var(--am-hero-glow), transparent 65%);
  content: "";
  pointer-events: none;
}

.grid {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(480px, 1fr);
  gap: clamp(52px, 6vw, 88px);
  align-items: start;
}

.copy {
  min-width: 0;
}

h1 {
  margin: 0 0 20px;
  color: var(--am-hero-text);
  font-size: clamp(56px, 5.2vw, 72px);
  line-height: 0.99;
  letter-spacing: -0.052em;
  font-weight: var(--am-font-bold);
}

.h1-sub {
  display: block;
  margin-top: 20px;
  color: var(--am-hero-text-muted);
  font-size: clamp(23px, 2.1vw, 29px);
  font-weight: var(--am-font-medium);
  line-height: 1.32;
  letter-spacing: -0.025em;
}

.lede {
  max-width: 56ch;
  margin: 0;
  color: var(--am-hero-text-muted);
  font-size: 18px;
  line-height: 1.66;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 14px;
}

.install {
  --am-technical-font-size: var(--am-text-body-sm);
  --am-technical-padding: 15px;
  margin-top: 30px;
  padding-block: 9px;
  box-shadow:
    0 20px 50px -30px color-mix(in srgb, var(--am-color-bg-canvas) 90%, transparent),
    inset 0 1px color-mix(in srgb, var(--am-color-text-primary) 3%, transparent);
}

.product-stage {
  position: relative;
  min-width: 0;
  padding: 0 14px 14px;
}

.preview-caption {
  margin: 12px 20px 2px;
  color: var(--am-hero-text-muted);
  font-size: var(--am-text-supporting);
  line-height: 1.5;
}

@media (max-width: 1020px) {
  .hero {
    padding-top: 104px;
  }

  .grid {
    grid-template-columns: 1fr;
    gap: 42px;
  }

  .copy {
    max-width: 720px;
  }

}

@media (max-width: 680px) {
  .hero {
    padding: 88px 0 64px;
    background-size: auto, 30px 30px, 30px 30px, auto;
  }

  h1 {
    font-size: 43px;
    letter-spacing: -0.045em;
  }

  .h1-sub {
    margin-top: 15px;
    font-size: 22px;
  }

  .lede {
    font-size: 16px;
  }

  .hero-action {
    flex: 1;
  }

  .install {
    --am-technical-font-size: var(--am-text-supporting);
    padding-block: 7px;
  }

}

</style>
