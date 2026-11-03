<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
import { isForwardNav, pathForTab, tabFromPath, viewHeadingId, viewTitle } from './lib/route'

const initial = tabFromPath(location.pathname)
// Normalise unknown or trailing-slash paths so the URL matches what is shown.
if (location.pathname !== pathForTab(initial)) {
  history.replaceState(null, '', pathForTab(initial) + location.search)
}
const tab = ref<Tab>(initial)
document.title = viewTitle(initial)

// Set by onPopState when Back/Forward changed the tab: the URL is already right and the
// browser restores scroll, so we must not push or scroll to the top.
let fromHistory = false

// Keep the URL in sync with the tab so /publish is linkable and Back works; announce the new view.
watch(tab, async (t) => {
  if (location.pathname !== pathForTab(t)) history.pushState(null, '', pathForTab(t))
  if (!fromHistory) window.scrollTo?.(0, 0)
  fromHistory = false
  document.title = viewTitle(t)
  await nextTick()
  document.getElementById(viewHeadingId(t))?.focus()
})
const onPopState = (e: PopStateEvent) => {
  const next = tabFromPath(location.pathname)
  if (next === tab.value) return
  fromHistory = !isForwardNav(e)
  tab.value = next
}
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
