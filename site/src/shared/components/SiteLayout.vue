<script setup lang="ts">
import { computed } from 'vue'
import { useData } from 'vitepress'
import DefaultTheme from 'vitepress/theme-without-fonts'

import SiteFooter from './SiteFooter.vue'

const { Layout } = DefaultTheme
const { frontmatter } = useData()
const isLanding = computed(() => frontmatter.value.layout === 'LandingPage')
</script>

<template>
  <div class="am-site" :class="isLanding ? 'am-landing' : 'am-docs'">
    <Layout>
      <template #layout-bottom>
        <SiteFooter />
      </template>
    </Layout>
  </div>
</template>
