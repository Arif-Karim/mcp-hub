<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import type { ApiClient } from '../api/client'
import type { CatalogueItem, Kind, SkillStatus } from '../api/types'
import { errorMessage } from '../lib/errors'
import { filterItems } from '../lib/filter'
import { pushToast } from '../lib/toast'
import ClaudeCallout from './ClaudeCallout.vue'
import DetailDrawer from './DetailDrawer.vue'
import ServerCard from './ServerCard.vue'
import SkillBanner from './SkillBanner.vue'

const props = withDefaults(defineProps<{ client: ApiClient; skill?: SkillStatus; debounceMs?: number }>(), {
  skill: undefined,
  debounceMs: 250,
})
const emit = defineEmits<{ changed: [] }>()

const items = ref<CatalogueItem[]>([])
const loading = ref(true)
const loadError = ref('')
const query = ref('')
const kind = ref<Kind | 'all'>('all')
const enabledOnly = ref(false)
const busy = reactive<Record<string, boolean>>({})
const errors = reactive<Record<string, string>>({})
const openId = ref<string | null>(null)

const kinds: { value: Kind | 'all'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'local', label: 'Local' },
  { value: 'remote', label: 'Remote' },
]

const visible = computed(() =>
  filterItems(items.value, { q: query.value, kind: kind.value, enabledOnly: enabledOnly.value }),
)
const openItem = computed(() => items.value.find((i) => i.id === openId.value))

let seq = 0
async function load() {
  const mine = ++seq
  try {
    // The daemon filters by q server-side; kind and enabled-only are applied locally so chips are instant.
    const result = await props.client.listCatalogue({ q: query.value.trim() || undefined })
    if (mine !== seq) return // a newer search superseded this one
    items.value = result
    loadError.value = ''
  } catch (e) {
    if (mine === seq) loadError.value = errorMessage(e)
  } finally {
    if (mine === seq) loading.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(query, () => {
  clearTimeout(timer)
  timer = setTimeout(load, props.debounceMs)
})
onMounted(load)
onBeforeUnmount(() => clearTimeout(timer))

async function toggle(item: CatalogueItem) {
  if (busy[item.id]) return
  busy[item.id] = true
  delete errors[item.id]
  const enabling = !item.state.enabled
  try {
    const updated = enabling ? await props.client.enable(item.id) : await props.client.disable(item.id)
    items.value = items.value.map((i) => (i.id === updated.id ? updated : i))
    pushToast(
      'success',
      enabling ? `${updated.name} enabled as ${updated.state.configName}` : `${updated.name} disabled — restart Claude Code to remove it`,
    )
    emit('changed')
  } catch (e) {
    const msg = errorMessage(e, item.id)
    errors[item.id] = msg
    pushToast('error', msg)
  } finally {
    delete busy[item.id]
  }
}
</script>

<template>
  <section aria-labelledby="catalogue-heading">
    <SkillBanner :skill="skill" :client="client" @installed="emit('changed')" />
    <ClaudeCallout v-if="skill?.installed" />

    <div class="toolbar">
      <h2 id="catalogue-heading" class="sr-only">Catalogue</h2>
      <label class="search">
        <span class="sr-only">Search servers</span>
        <input
          v-model="query"
          type="search"
          placeholder="Search by name, tag or tool — e.g. runbook"
          autocomplete="off"
          spellcheck="false"
        />
      </label>
      <div class="chips" role="group" aria-label="Filter by kind">
        <button
          v-for="k in kinds"
          :key="k.value"
          type="button"
          class="chip"
          :data-test="`chip-${k.value}`"
          :aria-pressed="kind === k.value"
          @click="kind = k.value"
        >
          {{ k.label }}
        </button>
        <label class="chip chip--check">
          <input v-model="enabledOnly" type="checkbox" data-test="enabled-only" />
          Enabled only
        </label>
      </div>
    </div>

    <p v-if="loadError" class="alert" role="alert">{{ loadError }}</p>

    <div v-if="loading" class="grid" aria-busy="true" aria-label="Loading catalogue">
      <div v-for="n in 3" :key="n" class="card card--skeleton" />
    </div>

    <p v-else-if="visible.length === 0 && !loadError" class="empty">
      No servers match these filters.
      <span class="muted">Try a broader search, or ask in #platform-engineering to get one added.</span>
    </p>

    <div v-else class="grid">
      <ServerCard
        v-for="item in visible"
        :key="item.id"
        :data-test="`card-${item.id}`"
        :item="item"
        :busy="!!busy[item.id]"
        :error="errors[item.id]"
        @toggle="toggle"
        @open="openId = $event.id"
      />
    </div>

    <DetailDrawer
      v-if="openItem"
      :item="openItem"
      :busy="!!busy[openItem.id]"
      :error="errors[openItem.id]"
      @toggle="toggle"
      @close="openId = null"
    />
  </section>
</template>
