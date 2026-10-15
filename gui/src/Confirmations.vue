<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { WorkspaceController } from './controller'
import GuiModal from './GuiModal.vue'

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

const choiceMade = ref(false)
const syncChoicesOffered = ref(false)

watch(
  quitOpen,
  (isOpen, wasOpen) => {
    if (isOpen) {
      choiceMade.value = false
      syncChoicesOffered.value = showSyncChoices.value
    } else if (wasOpen && !choiceMade.value) {
      props.controller.cancelQuit()
      if (syncChoicesOffered.value) {
        props.controller.recordSyncQuitCancellation()
      }
    }
  },
  { flush: 'sync', immediate: true },
)

watch(
  showSyncChoices,
  (offered) => {
    if (quitOpen.value && offered) {
      syncChoicesOffered.value = true
    }
  },
  { flush: 'sync' },
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
      choiceMade.value = true
      quitOpen.value = false
    }
  } finally {
    quitPending.value = false
  }
}

function quitNow() {
  choiceMade.value = true
  if (syncChoicesOffered.value) {
    props.controller.recordSyncQuitCancellation()
  }
  props.controller.quit()
  quitOpen.value = false
}

function cancelQuit() {
  choiceMade.value = true
  if (syncChoicesOffered.value) {
    props.controller.recordSyncQuitCancellation()
  }
  props.controller.cancelQuit()
  quitOpen.value = false
}
</script>

<template>
  <GuiModal
    v-model:open="migrationOpen"
    title="Migrate configuration"
    icon="sync"
    :dismissible="!migrationPending"
  >
    <p>Your user configuration uses an older format. Migrate it to the current format? Other configuration layers are left unchanged.</p>
    <p v-if="migrationError != null" role="alert">{{ migrationError }}</p>
    <p v-for="d in migrationDiagnostics" :key="d.code + d.message" role="alert">{{ d.message }}</p>
    <template #footer>
      <UButton
        label="Migrate"
        aria-label="Migrate"
        :disabled="migrationPending || state.operation != null || state.requesting"
        @click="migrate"
      />
      <UButton label="Leave unchanged" color="neutral" variant="outline" aria-label="Leave unchanged" :disabled="migrationPending" @click="dismissMigration" />
      <UButton label="Cancel" color="neutral" variant="outline" aria-label="Cancel migration offer" :disabled="migrationPending" @click="dismissMigration" />
    </template>
  </GuiModal>

  <GuiModal
    v-model:open="quitOpen"
    title="Quit Agent Marketplace"
    icon="quit"
    :dismissible="!quitPending"
  >
    <p v-if="syncRunning">A sync is running. Sync and quit when it finishes, quit now, or stay?</p>
    <p v-else-if="state.pendingChanges > 0">{{ state.pendingChanges }} change(s) not synced. Sync before quitting?</p>
    <template #footer>
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
    </template>
  </GuiModal>
</template>
