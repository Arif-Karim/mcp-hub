<script setup lang="ts">
import { ref } from 'vue'
import { cap } from '../lib/labels'
import { GUIDE_URL, TEMPLATE_URL, exampleEntry, kinds, lifecycle, steps } from '../lib/publish'

const copyStatus = ref('')

async function copy() {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(exampleEntry)
    copyStatus.value = 'Copied to clipboard'
  } catch {
    copyStatus.value = "Couldn't copy — select the text and copy it manually"
  }
}
</script>

<template>
  <article class="publish" aria-labelledby="publish-heading">
    <header class="publish__intro">
      <h2 id="publish-heading">Publish your MCP server</h2>
      <p class="publish__pitch">
        Put your service’s tools in every engineer’s Claude Code. Local servers get a short-lived, identity-aware token from
        the hub, so no secrets go in anyone’s config, and your API keeps its own access control.
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
            <tr><th scope="col">Kind</th><th scope="col">What the hub does</th><th scope="col">Use it when</th></tr>
          </thead>
          <tbody>
            <tr v-for="k in kinds" :key="k.kind">
              <th scope="row">{{ k.kind }}</th>
              <td>{{ k.does }}</td>
              <td>{{ k.when }}</td>
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
        <button type="button" class="btn btn--small" data-test="copy" @click="copy">Copy</button>
      </div>
      <pre class="snippet" tabindex="0" aria-label="Example catalogue entry (YAML)" data-test="example">{{ exampleEntry }}</pre>
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
