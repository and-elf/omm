<script setup lang="ts">
import cytoscape, { type Core } from 'cytoscape'
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'

import { api, ApiClient } from '@/api/client'
import { toElements } from '@/utils/topology'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const container = ref<HTMLDivElement | null>(null)
const cy = shallowRef<Core | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const empty = ref(false)

const style: cytoscape.StylesheetStyle[] = [
  {
    selector: 'node',
    style: {
      label: 'data(label)',
      'background-color': '#38bdf8',
      color: '#e2e8f0',
      'font-size': 9,
      'text-valign': 'bottom',
      'text-margin-y': 4,
      width: 26,
      height: 26,
    },
  },
  { selector: '.node--self', style: { 'background-color': '#f59e0b', width: 34, height: 34 } },
  {
    selector: '.client',
    style: { 'background-color': '#64748b', shape: 'round-rectangle', width: 18, height: 18 },
  },
  {
    selector: 'edge',
    style: {
      label: 'data(label)',
      'font-size': 8,
      color: '#94a3b8',
      'curve-style': 'bezier',
      'text-rotation': 'autorotate',
      width: 2,
    },
  },
  { selector: '.link--good, .assoc--good', style: { 'line-color': '#4ade80' } },
  { selector: '.link--fair, .assoc--fair', style: { 'line-color': '#facc15' } },
  { selector: '.link--weak, .assoc--weak', style: { 'line-color': '#f87171' } },
  { selector: '.assoc', style: { 'line-style': 'dashed' } },
]

async function load() {
  loading.value = true
  error.value = null
  try {
    const topo = await props.client.getTopology()
    const elements = toElements(topo)
    empty.value = elements.length <= 1
    if (cy.value) {
      cy.value.json({ elements })
      cy.value.layout({ name: 'cose', animate: false }).run()
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (container.value) {
    cy.value = cytoscape({ container: container.value, style, elements: [] })
  }
  load()
})

onBeforeUnmount(() => cy.value?.destroy())
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Topology</h2>
      <button class="btn" :disabled="loading" @click="load">Refresh</button>
    </header>

    <p v-if="error" class="async__state async__state--error" role="alert">{{ error }}</p>
    <p v-else-if="empty && !loading" class="async__state">
      No mesh links or clients reported yet. Links appear as batman-adv neighbours come up and
      clients associate.
    </p>

    <div ref="container" class="topology" :class="{ 'topology--hidden': error }"></div>

    <div class="legend">
      <span><i class="dot dot--self"></i> this device</span>
      <span><i class="dot dot--node"></i> mesh node</span>
      <span><i class="dot dot--client"></i> client</span>
      <span><i class="line line--good"></i> good</span>
      <span><i class="line line--fair"></i> fair</span>
      <span><i class="line line--weak"></i> weak</span>
    </div>
  </section>
</template>

<style scoped>
.topology {
  height: 460px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 0.75rem;
}
.topology--hidden {
  display: none;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  margin-top: 0.75rem;
  color: var(--muted);
  font-size: 0.85rem;
  align-items: center;
}
.legend span {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
}
.dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  display: inline-block;
}
.dot--self {
  background: #f59e0b;
}
.dot--node {
  background: #38bdf8;
}
.dot--client {
  background: #64748b;
  border-radius: 3px;
}
.line {
  width: 16px;
  height: 3px;
  display: inline-block;
}
.line--good {
  background: #4ade80;
}
.line--fair {
  background: #facc15;
}
.line--weak {
  background: #f87171;
}
</style>
