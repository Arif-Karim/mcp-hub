<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogueItem } from '../api/types'
import { blockedReason, cap, statusLabel } from '../lib/labels'

const props = defineProps<{ item: CatalogueItem; busy?: boolean; error?: string }>()
const emit = defineEmits<{ toggle: [item: CatalogueItem]; open: [item: CatalogueItem] }>()

const reason = computed(() => blockedReason[props.item.approval])
const label = computed(() => {
  const on = props.item.state.enabled
  if (props.busy) return on ? 'Disabling…' : 'Enabling…'
  return on ? 'Disable' : 'Enable'
})
</script>

<template>
  <article class="card" :class="{ 'card--on': item.state.enabled, 'card--blocked': reason }">
    <header class="card__head">
      <h3 class="card__title">
        <button type="button" class="linkish" data-test="open" @click="emit('open', item)">{{ item.name }}</button>
      </h3>
      <span v-if="item.state.enabled" class="status" :class="`status--${item.state.status}`" data-test="status">
        {{ statusLabel[item.state.status] }}
      </span>
    </header>
    <p class="card__owner">{{ item.owner }}</p>
    <p class="card__desc">{{ item.description }}</p>
    <ul class="badges" aria-label="Attributes">
      <li class="badge badge--kind" data-test="badge-kind">{{ cap(item.kind) }}</li>
      <li class="badge" :class="`badge--${item.classification}`" data-test="badge-classification">
        {{ cap(item.classification) }}
      </li>
      <li class="badge" :class="`badge--${item.approval}`" data-test="badge-approval">{{ cap(item.approval) }}</li>
    </ul>
    <p v-if="error" class="alert" role="alert">{{ error }}</p>
    <footer class="card__foot">
      <button
        type="button"
        class="btn"
        :class="item.state.enabled ? 'btn--ghost' : 'btn--primary'"
        data-test="toggle"
        :disabled="busy || !!reason"
        :aria-busy="busy ? 'true' : 'false'"
        :aria-label="`${label} ${item.name}`"
        @click="emit('toggle', item)"
      >
        <span v-if="busy" class="spinner" aria-hidden="true"></span>{{ label }}
      </button>
      <span v-if="reason" class="card__reason" data-test="reason">{{ reason }}</span>
    </footer>
  </article>
</template>
