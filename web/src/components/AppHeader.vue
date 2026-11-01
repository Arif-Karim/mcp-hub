<script setup lang="ts">
import type { Status } from '../api/types'

export type Tab = 'catalogue' | 'activity'

defineProps<{ status?: Status; unreachable: boolean; tab: Tab }>()
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
      <p v-if="unreachable" class="pill pill--bad" role="status">
        <span class="dot" aria-hidden="true"></span>Daemon unreachable
      </p>
      <p v-else-if="status" class="pill pill--ok" role="status" :title="`Listening on ${status.addr}`">
        <span class="dot" aria-hidden="true"></span>
        v{{ status.version }} · {{ status.enabledCount }} enabled · {{ status.user }}
      </p>
      <p v-else class="pill" role="status">Connecting…</p>
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
