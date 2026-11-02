<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from './api'
import type { Status } from './api/types'
import { daemonHealth } from './lib/status'
import type { DaemonHealth } from './lib/status'
import ActivityView from './components/ActivityView.vue'
import AppHeader from './components/AppHeader.vue'
import type { Tab } from './components/AppHeader.vue'
import CatalogueView from './components/CatalogueView.vue'
import PublishView from './components/PublishView.vue'
import ToastStack from './components/ToastStack.vue'
import { pathForTab, tabFromPath } from './lib/route'

const tab = ref<Tab>(tabFromPath(location.pathname))
// Keep the URL in sync with the tab so /publish is linkable and Back works.
watch(tab, (t) => {
  if (location.pathname !== pathForTab(t)) history.pushState(null, '', pathForTab(t))
  window.scrollTo?.(0, 0)
})
const onPopState = () => (tab.value = tabFromPath(location.pathname))
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
  window.addEventListener('popstate', onPopState)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  window.removeEventListener('popstate', onPopState)
})
</script>

<template>
  <AppHeader v-model:tab="tab" :status="status" :health="health" />
  <main class="main">
    <KeepAlive include="CatalogueView">
      <CatalogueView v-if="tab === 'catalogue'" :client="api" :skill="status?.skill" @changed="refreshStatus" />
      <ActivityView v-else-if="tab === 'activity'" :client="api" />
      <PublishView v-else />
    </KeepAlive>
  </main>
  <ToastStack />
</template>
