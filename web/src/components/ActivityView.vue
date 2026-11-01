<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { ApiClient } from '../api/client'
import type { AuditEvent } from '../api/types'
import { errorMessage } from '../lib/errors'

const props = withDefaults(defineProps<{ client: ApiClient; intervalMs?: number }>(), { intervalMs: 5000 })

const events = ref<AuditEvent[]>([])
const loading = ref(true)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined
let inFlight = false

async function load() {
  if (inFlight) return // a slow daemon must not pile up overlapping requests
  inFlight = true
  try {
    events.value = await props.client.audit(100)
    error.value = ''
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    inFlight = false
    loading.value = false
  }
}

onMounted(() => {
  load()
  timer = setInterval(load, props.intervalMs)
})
onBeforeUnmount(() => clearInterval(timer))

const fmtTime = (iso: string) =>
  new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
const statusClass = (s: number) => (s >= 500 ? 'bad' : s >= 400 ? 'warn' : 'ok')
</script>

<template>
  <section aria-labelledby="activity-heading">
    <div class="section-head">
      <h2 id="activity-heading">Activity</h2>
      <span class="muted live"><span class="dot" aria-hidden="true"></span>Auto-refreshing every 5s</span>
    </div>

    <p v-if="error" class="alert" role="alert">{{ error }}</p>

    <p v-if="loading" class="muted" aria-busy="true">Loading activity…</p>

    <div v-else-if="events.length === 0 && !error" class="empty">
      <p>Tool calls made by Claude Code through mcp-hub appear here.</p>
      <p class="muted">Arguments and results are never recorded.</p>
    </div>

    <div v-else class="table-wrap">
      <table>
        <caption class="sr-only">Recent proxied MCP requests, newest first. Arguments and results are never recorded.</caption>
        <thead>
          <tr>
            <th scope="col">Time</th>
            <th scope="col">Server</th>
            <th scope="col">Method</th>
            <th scope="col">Tool</th>
            <th scope="col">Status</th>
            <th scope="col" class="num">Duration</th>
            <th scope="col">User</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(e, i) in events" :key="`${e.time}-${e.server}-${i}`">
            <td><time :datetime="e.time">{{ fmtTime(e.time) }}</time></td>
            <td><code>{{ e.server }}</code></td>
            <td>{{ e.method }}</td>
            <td><code v-if="e.tool">{{ e.tool }}</code><span v-else class="muted">—</span></td>
            <td><span class="code-pill" :class="`code-pill--${statusClass(e.status)}`">{{ e.status }}</span></td>
            <td class="num">{{ e.durationMs }} ms</td>
            <td class="muted">{{ e.user }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
