<script setup lang="ts">
import type { Status } from '../api/types'
import type { DaemonHealth } from '../lib/status'

export type Tab = 'catalogue' | 'activity'

defineProps<{ health: DaemonHealth; status?: Status; tab: Tab }>()
const emit = defineEmits<{ 'update:tab': [tab: Tab] }>()

const tabs: { id: Tab; label: string }[] = [
  { id: 'catalogue', label: 'Catalogue' },
  { id: 'activity', label: 'Activity' },
]
</script>

<template>
  <header class="header">
    <div class="header__row">
      <h1 class="brand"><span class="brand__mark" aria-hidden="true">⌘</span>mcp-hub</h1>
      <p v-if="health === 'unreachable'" class="pill pill--bad" role="status" data-test="status-pill">
        <span class="dot" aria-hidden="true"></span>Daemon unreachable
      </p>
      <p
        v-else-if="health === 'degraded'"
        class="pill pill--warn"
        role="status"
        data-test="status-pill"
        title="The daemon is running but /status returned an error"
      >
        <span class="dot" aria-hidden="true"></span>Status unavailable
      </p>
      <p
        v-else-if="health === 'ok' && status"
        class="pill pill--ok"
        role="status"
        data-test="status-pill"
        :title="`Listening on ${status.addr}`"
      >
        <span class="dot" aria-hidden="true"></span>
        v{{ status.version }} · {{ status.enabledCount }} enabled · {{ status.user }}
      </p>
      <p v-else class="pill" role="status" data-test="status-pill">Connecting…</p>
    </div>
    <nav class="tabs" aria-label="Sections">
      <button
        v-for="t in tabs"
        :key="t.id"
        type="button"
        class="tab"
        :aria-current="tab === t.id ? 'page' : undefined"
        @click="emit('update:tab', t.id)"
      >
        {{ t.label }}
      </button>
    </nav>
  </header>
</template>
