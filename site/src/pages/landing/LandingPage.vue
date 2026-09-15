<script setup lang="ts">
import './landing.css'

import HeroSection from './sections/HeroSection.vue'
import CompatibilityStrip from './components/CompatibilityStrip.vue'
import WorkflowSection from './sections/WorkflowSection.vue'
import NativeOutputSection from './sections/NativeOutputSection.vue'
import ScopesPolicySection from './sections/ScopesPolicySection.vue'
import SafeSyncSection from './sections/SafeSyncSection.vue'
import GetStartedSection from './sections/GetStartedSection.vue'
</script>

<template>
  <div class="landing">
    <HeroSection />
    <CompatibilityStrip />
    <WorkflowSection />
    <NativeOutputSection />
    <ScopesPolicySection />
    <SafeSyncSection />
    <GetStartedSection />
  </div>
</template>
