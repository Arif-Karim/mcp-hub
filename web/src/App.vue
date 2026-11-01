<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from './api'
import type { Status } from './api/types'
import ActivityView from './components/ActivityView.vue'
import AppHeader from './components/AppHeader.vue'
import type { Tab } from './components/AppHeader.vue'
import CatalogueView from './components/CatalogueView.vue'
import ToastStack from './components/ToastStack.vue'

const tab = ref<Tab>('catalogue')
const status = ref<Status>()
const unreachable = ref(false)

async function refreshStatus() {
  try {
    status.value = await api.status()
    unreachable.value = false
  } catch {
    unreachable.value = true
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
  <AppHeader v-model:tab="tab" :status="status" :unreachable="unreachable" />
  <main class="main">
    <KeepAlive include="CatalogueView">
      <CatalogueView v-if="tab === 'catalogue'" :client="api" @changed="refreshStatus" />
      <ActivityView v-else :client="api" />
    </KeepAlive>
  </main>
  <ToastStack />
</template>
