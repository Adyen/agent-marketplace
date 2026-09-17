<script setup lang="ts">
import DefaultTheme from 'vitepress/theme-without-fonts'

import SiteFooter from './SiteFooter.vue'
import DocsVersionBanner from './DocsVersionBanner.vue'

const { Layout } = DefaultTheme
</script>

<template>
  <Layout>
    <template #doc-before>
      <DocsVersionBanner />
    </template>
    <template #layout-bottom>
      <SiteFooter />
    </template>
  </Layout>
</template>
