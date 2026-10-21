<script setup lang="ts">
import DefaultTheme from 'vitepress/theme-without-fonts'

import SiteFooter from './SiteFooter.vue'

const { Layout } = DefaultTheme
</script>

<template>
  <div class="am-site">
    <Layout>
      <template #layout-bottom>
        <SiteFooter />
      </template>
    </Layout>
  </div>
</template>
