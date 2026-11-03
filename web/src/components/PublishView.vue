<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { cap } from '../lib/labels'
import { GUIDE_URL, TEMPLATE_URL, exampleEntry, kinds, lifecycle, steps } from '../lib/publish'

const copyStatus = ref('')
const pre = ref<HTMLPreElement>()

async function copy() {
  // Clear first so a repeated result is announced again by the live region.
  copyStatus.value = ''
  await nextTick()
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(exampleEntry)
    copyStatus.value = 'Copied to clipboard'
  } catch {
    // Select the snippet so a manual copy is one keystroke.
    if (pre.value) window.getSelection()?.selectAllChildren(pre.value)
    copyStatus.value = "Couldn't copy — select the text and copy it manually"
  }
}
</script>

<template>
  <article class="publish" aria-labelledby="publish-heading">
    <header class="publish__intro">
      <h2 id="publish-heading" tabindex="-1">Publish your MCP server</h2>
      <p class="publish__pitch">
        Put your service’s tools in every engineer’s Claude Code. Local servers get a short-lived, identity-aware token from
        the hub, so no secrets go in anyone’s config, and your API keeps its own access control.
      </p>
      <p class="callout" data-test="ask-claude">
        <span>
          Or just ask Claude Code: <q>help me build an MCP server for my team's service</q> — the mcp-hub skills
          (<code>mcp-hub-build-server</code>, <code>mcp-hub-publish-server</code>) walk you through building and
          publishing.
        </span>
      </p>
      <p class="publish__links">
        <a :href="GUIDE_URL" target="_blank" rel="noopener noreferrer" data-test="guide-link">Full publishing guide ↗</a>
        <a :href="TEMPLATE_URL" target="_blank" rel="noopener noreferrer" data-test="template-link">Server template ↗</a>
      </p>
    </header>

    <section aria-labelledby="kinds-heading">
      <h3 id="kinds-heading">Local or remote?</h3>
      <div class="table-wrap">
        <table class="publish__table" data-test="kinds">
          <thead>
            <tr role="row">
              <th scope="col" role="columnheader">Kind</th>
              <th scope="col" role="columnheader">What the hub does</th>
              <th scope="col" role="columnheader">Use it when</th>
            </tr>
          </thead>
          <tbody>
            <!-- Explicit roles keep table semantics when the narrow layout restyles rows as grids. -->
            <tr v-for="k in kinds" :key="k.kind" role="row">
              <th scope="row" role="rowheader">{{ k.kind }}</th>
              <td role="cell">{{ k.does }}</td>
              <td role="cell">{{ k.when }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section aria-labelledby="steps-heading">
      <h3 id="steps-heading">Five steps to the catalogue</h3>
      <ol class="steps" data-test="steps">
        <li v-for="s in steps" :key="s.title" class="step">
          <h4 class="step__title">{{ s.title }}</h4>
          <p>{{ s.body }}<template v-if="s.code"> <code>{{ s.code }}</code>.</template></p>
        </li>
      </ol>
    </section>

    <section aria-labelledby="example-heading">
      <div class="section-head">
        <h3 id="example-heading">Example catalogue entry</h3>
        <button type="button" class="btn btn--small" data-test="copy" aria-label="Copy example catalogue entry" @click="copy">
          Copy
        </button>
      </div>
      <pre ref="pre" class="snippet" tabindex="0" aria-label="Example catalogue entry (YAML)" data-test="example">{{ exampleEntry }}</pre>
      <p class="muted note" aria-live="polite" data-test="copy-status">{{ copyStatus }}</p>
    </section>

    <section aria-labelledby="lifecycle-heading">
      <h3 id="lifecycle-heading">Lifecycle</h3>
      <dl class="lifecycle" data-test="lifecycle">
        <div v-for="l in lifecycle" :key="l.state" class="lifecycle__row">
          <dt><span class="badge" :class="`badge--${l.state}`">{{ cap(l.state) }}</span></dt>
          <dd>{{ l.seen }} <span class="muted">Set by: {{ l.who }}.</span></dd>
        </div>
      </dl>
    </section>

    <p class="callout publish__demo">
      Demo: GitHub Releases stands in for Artifactory and the catalogue ships inside mcp-hub, so a merged entry reaches
      engineers with the next mcp-hub release.
    </p>
  </article>
</template>
