<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { WorkspaceController } from './controller'

const props = defineProps<{ controller: WorkspaceController }>()
const quitOpen = defineModel<boolean>('quitOpen', { default: false })

const migrationOpen = ref(false)
const migrationSeen = ref(false)
const migrationPending = ref(false)
const migrationError = ref<string | null>(null)
const quitPending = ref(false)

const state = computed(() => props.controller.state)
const migrationDiagnostics = computed(() =>
  state.value.diagnostics.filter((d) => d.category === 'migration' && d.severity === 'error'),
)
const syncRunning = computed(() => state.value.operation?.kind === 'sync')
const showSyncChoices = computed(() => state.value.pendingChanges > 0 || syncRunning.value)
const syncQuitBlocked = computed(
  () => state.value.requesting || (state.value.operation != null && !syncRunning.value) || quitPending.value,
)

watch(
  () => state.value.migrationOffered,
  (offered, was) => {
    if (offered && !migrationSeen.value) {
      migrationSeen.value = true
      migrationOpen.value = true
    }
    if (was && !offered) {
      migrationOpen.value = false
      migrationPending.value = false
      migrationError.value = null
    }
  },
  { immediate: true },
)

async function migrate() {
  if (migrationPending.value) {
    return
  }
  migrationPending.value = true
  migrationError.value = null
  try {
    if (!(await props.controller.runAction('migrate'))) {
      migrationError.value = state.value.error ?? 'The action could not be started'
    }
  } finally {
    migrationPending.value = false
  }
}

function dismissMigration() {
  migrationOpen.value = false
}

async function syncAndQuit() {
  if (syncQuitBlocked.value) {
    return
  }
  quitPending.value = true
  try {
    const accepted = await props.controller.syncAndQuit()
    if (accepted) {
      quitOpen.value = false
    }
  } finally {
    quitPending.value = false
  }
}

function quitNow() {
  props.controller.quit()
  quitOpen.value = false
}

function cancelQuit() {
  props.controller.cancelQuit()
  quitOpen.value = false
}
</script>

<template>
  <UModal v-model:open="migrationOpen" title="Migrate configuration" aria-label="Migrate configuration" :dismissible="!migrationPending">
    <template #content>
      <section class="settings" aria-label="Migrate configuration">
        <h2 class="settings__heading">Migrate configuration</h2>
        <p>Your user configuration uses an older format. Migrate it to the current format? Other configuration layers are left unchanged.</p>
        <p v-if="migrationError != null" role="alert">{{ migrationError }}</p>
        <p v-for="d in migrationDiagnostics" :key="d.code + d.message" role="alert">{{ d.message }}</p>
        <div class="settings__actions">
          <UButton
            label="Migrate"
            aria-label="Migrate"
            :disabled="migrationPending || state.operation != null || state.requesting"
            @click="migrate"
          />
          <UButton label="Leave unchanged" color="neutral" variant="outline" aria-label="Leave unchanged" :disabled="migrationPending" @click="dismissMigration" />
          <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel migration offer" :disabled="migrationPending" @click="dismissMigration" />
        </div>
      </section>
    </template>
  </UModal>

  <UModal v-model:open="quitOpen" title="Quit Agent Marketplace" aria-label="Quit Agent Marketplace" :dismissible="!quitPending">
    <template #content>
      <section class="settings" aria-label="Quit Agent Marketplace">
        <h2 class="settings__heading">Quit Agent Marketplace</h2>
        <p v-if="syncRunning">A sync is running. Sync and quit when it finishes, quit now, or stay?</p>
        <p v-else-if="state.pendingChanges > 0">{{ state.pendingChanges }} change(s) not synced. Sync before quitting?</p>
        <div class="settings__actions">
          <template v-if="showSyncChoices">
            <UButton label="Sync and quit" aria-label="Sync and quit" :disabled="syncQuitBlocked" @click="syncAndQuit" />
            <UButton label="Quit without syncing" color="neutral" variant="outline" aria-label="Quit without syncing" :disabled="quitPending" @click="quitNow" />
            <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel quit" @click="cancelQuit" />
          </template>
          <template v-else>
            <UButton label="Quit" aria-label="Quit now" :disabled="quitPending" @click="quitNow" />
            <UButton label="Stay" color="neutral" variant="outline" aria-label="Stay" @click="cancelQuit" />
            <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel quit" @click="cancelQuit" />
          </template>
        </div>
      </section>
    </template>
  </UModal>
</template>
