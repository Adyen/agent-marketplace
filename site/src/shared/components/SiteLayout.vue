<script setup lang="ts">
import DefaultTheme from 'vitepress/theme-without-fonts'

import CopyPageMenu from './CopyPageMenu.vue'
import SiteFooter from './SiteFooter.vue'

const { Layout } = DefaultTheme
</script>

<template>
  <div class="am-site">
    <Layout>
      <template #doc-before>
        <CopyPageMenu />
      </template>
      <template #layout-bottom>
        <SiteFooter />
      </template>
    </Layout>
  </div>
</template>
