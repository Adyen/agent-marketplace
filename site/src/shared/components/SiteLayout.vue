<script setup lang="ts">
import DefaultTheme from 'vitepress/theme-without-fonts'

import SiteFooter from './SiteFooter.vue'

const { Layout } = DefaultTheme
</script>

<template>
  <Layout>
    <template #layout-bottom>
      <SiteFooter />
    </template>
  </Layout>
</template>
