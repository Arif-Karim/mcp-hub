<script setup lang="ts">
import { dismissToast, toasts } from '../lib/toast'
</script>

<template>
  <div class="toasts" role="status" aria-live="polite" aria-relevant="additions">
    <div v-for="t in toasts" :key="t.id" class="toast" :class="`toast--${t.kind}`">
      <span>{{ t.text }}</span>
      <button type="button" class="icon-btn" aria-label="Dismiss notification" @click="dismissToast(t.id)">✕</button>
    </div>
  </div>
</template>
