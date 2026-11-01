<script setup lang="ts">
import { ref } from 'vue'

const KEY = 'mcp-hub.callout.dismissed'

function readDismissed(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

const hidden = ref(readDismissed())

function dismiss() {
  hidden.value = true
  try {
    localStorage.setItem(KEY, '1')
  } catch {
    // Storage unavailable (private mode) — dismissal just won't persist.
  }
}
</script>

<template>
  <aside v-if="!hidden" class="callout" aria-label="Tip">
    <span class="callout__mark" aria-hidden="true">✳</span>
    <p>
      <strong>Use mcp-hub from Claude Code</strong> — ask:
      <em>“find me an MCP server for service ownership”</em> — the mcp-hub skill searches this catalogue and enables
      servers for you.
    </p>
    <button type="button" class="icon-btn" aria-label="Dismiss tip" @click="dismiss">✕</button>
  </aside>
</template>
