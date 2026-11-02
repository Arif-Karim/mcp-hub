<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from './api'
import type { Status } from './api/types'
import { daemonHealth } from './lib/status'
import type { DaemonHealth } from './lib/status'
import ActivityView from './components/ActivityView.vue'
import AppHeader from './components/AppHeader.vue'
import type { Tab } from './components/AppHeader.vue'
import CatalogueView from './components/CatalogueView.vue'
import ToastStack from './components/ToastStack.vue'

const tab = ref<Tab>('catalogue')
const status = ref<Status>()
const health = ref<DaemonHealth>('loading')

async function refreshStatus() {
  try {
    status.value = await api.status()
    health.value = 'ok'
  } catch (e) {
    health.value = daemonHealth(e)
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  refreshStatus()
  timer = setInterval(refreshStatus, 10_000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <AppHeader v-model:tab="tab" :status="status" :health="health" />
  <main class="main">
    <KeepAlive include="CatalogueView">
      <CatalogueView v-if="tab === 'catalogue'" :client="api" :skill="status?.skill" @changed="refreshStatus" />
      <ActivityView v-else :client="api" />
    </KeepAlive>
  </main>
  <ToastStack />
</template>
