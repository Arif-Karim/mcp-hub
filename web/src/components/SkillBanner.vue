<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ApiClient } from '../api/client'
import type { SkillStatus } from '../api/types'
import { errorMessage } from '../lib/errors'

const props = defineProps<{ skill?: SkillStatus; client: ApiClient }>()
const emit = defineEmits<{ installed: [] }>()

const dismissed = ref(false) // per page load: the problem is real, so it comes back next visit
const busy = ref(false)
const done = ref(false)
const error = ref('')

const outdated = computed(() => props.skill?.installed === true && !props.skill.upToDate)
const needsAction = computed(() => props.skill !== undefined && (!props.skill.installed || !props.skill.upToDate))
const visible = computed(() => !dismissed.value && (done.value || needsAction.value))

async function install() {
  busy.value = true
  error.value = ''
  try {
    await props.client.installSkill()
    done.value = true
    emit('installed')
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <aside
    v-if="visible"
    class="callout"
    :class="done ? 'callout--ok' : 'callout--warn'"
    aria-label="Claude Code skill"
    data-test="skill-banner"
  >
    <span class="callout__mark" aria-hidden="true">{{ done ? '✓' : '!' }}</span>
    <div class="callout__body">
      <p v-if="done" role="status">
        <strong>Installed — restart Claude Code or run <code>/reload-plugins</code></strong> to start using the mcp-hub
        skill.
      </p>
      <p v-else-if="outdated">
        <strong>Claude Code skill out of date</strong> — this mcp-hub has a newer version of the skill Claude uses to
        find and enable servers.
      </p>
      <p v-else>
        <strong>Claude Code skill not installed — Claude can't find or enable servers for you until it is.</strong>
      </p>
      <p v-if="error" class="callout__error" role="alert">{{ error }}</p>
    </div>
    <button
      v-if="!done"
      type="button"
      class="btn btn--primary"
      data-test="skill-install"
      :disabled="busy"
      :aria-busy="busy"
      @click="install"
    >
      {{ busy ? 'Installing…' : outdated ? 'Update skill' : 'Install skill' }}
    </button>
    <button type="button" class="icon-btn" aria-label="Dismiss" data-test="skill-dismiss" @click="dismissed = true">
      ✕
    </button>
  </aside>
</template>
