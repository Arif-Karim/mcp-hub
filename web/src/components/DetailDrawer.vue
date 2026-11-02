<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { CatalogueItem } from '../api/types'
import { authExplainer, blockedReason, cap, statusLabel } from '../lib/labels'
import { pushToast } from '../lib/toast'
import { safeDocsUrl } from '../lib/url'

const props = defineProps<{ item: CatalogueItem; busy?: boolean; error?: string }>()
const emit = defineEmits<{ close: []; toggle: [item: CatalogueItem] }>()

const dialog = ref<HTMLElement>()
const closeBtn = ref<HTMLButtonElement>()
const reason = computed(() => blockedReason[props.item.approval])
const docsHref = computed(() => safeDocsUrl(props.item.docsUrl))
const titleId = computed(() => `drawer-title-${props.item.id}`)
let returnFocus: Element | null = null

// Esc is handled at window level so it still works if focus left the drawer
// (e.g. the toggle button was disabled while a request was in flight).
function onWindowKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(async () => {
  returnFocus = document.activeElement
  window.addEventListener('keydown', onWindowKeydown)
  await nextTick()
  closeBtn.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
  if (returnFocus instanceof HTMLElement) returnFocus.focus()
})

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Tab' || !dialog.value) return
  // Keep focus inside the drawer while it is open.
  const focusable = dialog.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), a[href], input, [tabindex]:not([tabindex="-1"])',
  )
  if (focusable.length === 0) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first.focus()
  }
}

async function copy(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    pushToast('success', `Copied ${what}`, 2500)
  } catch {
    pushToast('error', `Couldn't copy — select the ${what} and copy it manually.`)
  }
}
</script>

<template>
  <div class="scrim" @click.self="emit('close')">
    <aside
      ref="dialog"
      class="drawer"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      @keydown="onKeydown"
    >
      <header class="drawer__head">
        <div>
          <h2 :id="titleId" class="drawer__title">{{ item.name }}</h2>
          <p class="muted">
            {{ item.owner }} · v{{ item.version }} · {{ cap(item.kind) }}
            <template v-if="item.state.enabled"> · {{ statusLabel[item.state.status] }}</template>
          </p>
        </div>
        <button ref="closeBtn" type="button" class="icon-btn" data-test="close" aria-label="Close details" @click="emit('close')">
          ✕
        </button>
      </header>

      <div class="drawer__body">
        <p>{{ item.description }}</p>

        <ul class="badges" aria-label="Attributes">
          <li class="badge" :class="`badge--${item.classification}`">{{ cap(item.classification) }}</li>
          <li class="badge" :class="`badge--${item.approval}`">{{ cap(item.approval) }}</li>
          <li v-for="t in item.tags" :key="t" class="badge badge--tag">#{{ t }}</li>
        </ul>

        <p v-if="error" class="alert" role="alert">{{ error }}</p>

        <section v-if="item.state.enabled" class="panel panel--ok" aria-label="Claude Code configuration">
          <dl class="kv">
            <dt>Config name</dt>
            <dd>
              <code data-test="config-name">{{ item.state.configName }}</code>
              <button type="button" class="btn btn--small" data-test="copy" @click="copy(item.state.configName, 'config name')">
                Copy
              </button>
            </dd>
            <template v-if="item.state.proxyUrl">
              <dt>Proxy URL</dt>
              <dd>
                <code data-test="proxy-url">{{ item.state.proxyUrl }}</code>
                <button type="button" class="btn btn--small" data-test="copy" @click="copy(item.state.proxyUrl, 'proxy URL')">
                  Copy
                </button>
              </dd>
            </template>
          </dl>
          <p class="note">Run <code>/reload-plugins</code> in Claude Code (or restart it) to load the new server.</p>
        </section>

        <h3>Authentication</h3>
        <p>
          <code>{{ item.auth }}</code> — {{ authExplainer[item.auth] }}
        </p>

        <h3>Tools ({{ item.tools.length }})</h3>
        <ul class="tools">
          <li v-for="t in item.tools" :key="t.name">
            <code>{{ t.name }}</code>
            <span class="muted">{{ t.description }}</span>
          </li>
        </ul>

        <p v-if="docsHref">
          <a :href="docsHref" data-test="docs" target="_blank" rel="noopener noreferrer">Documentation ↗</a>
        </p>
      </div>

      <footer class="drawer__foot">
        <button
          type="button"
          class="btn"
          :class="item.state.enabled ? 'btn--ghost' : 'btn--primary'"
          :disabled="busy || !!reason"
          :aria-busy="busy ? 'true' : 'false'"
          @click="emit('toggle', item)"
        >
          <span v-if="busy" class="spinner" aria-hidden="true"></span>
          {{ busy ? (item.state.enabled ? 'Disabling…' : 'Enabling…') : item.state.enabled ? 'Disable' : 'Enable' }}
        </button>
        <span v-if="reason" class="card__reason">{{ reason }}</span>
      </footer>
    </aside>
  </div>
</template>
