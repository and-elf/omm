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
  // Backhaul type: a solid border marks a wired node, a dashed border a
  // wireless one, so an operator can see how each node reaches the mesh.
  { selector: '.node--eth', style: { 'border-width': 3, 'border-color': '#22d3ee', 'border-style': 'solid' } },
  { selector: '.node--wifi', style: { 'border-width': 3, 'border-color': '#a78bfa', 'border-style': 'dashed' } },
  // A node running as multi-AP (802.11s degraded/unavailable) is tinted amber so
  // the fallback stands out per node.
  { selector: '.node--multiap', style: { 'background-color': '#f59e0b' } },
  // Liveness: a node the controller has stopped hearing from is onboarded but not
  // alive (#29). Stale (recently overdue) is dimmed grey; down (long silent) is
  // greyed out with a red dashed border, its label already suffixed with ✕.
  { selector: '.node--stale', style: { 'background-color': '#64748b', opacity: 0.55, 'text-opacity': 0.7 } },
  {
    selector: '.node--down',
    style: {
      'background-color': '#475569',
      'border-width': 3,
      'border-color': '#ef4444',
      'border-style': 'dashed',
      opacity: 0.5,
      color: '#fca5a5',
    },
  },
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
  // Quality colour by RSSI/TQ tier (shared by mesh links and client assocs).
  { selector: '.link--excellent, .assoc--excellent', style: { 'line-color': '#4ade80' } },
  { selector: '.link--good, .assoc--good', style: { 'line-color': '#86efac' } },
  { selector: '.link--fair, .assoc--fair', style: { 'line-color': '#facc15' } },
  { selector: '.link--weak, .assoc--weak', style: { 'line-color': '#f87171' } },
  // Backhaul medium: wired links draw solid (cyan, matching the wired node
  // border), wireless links draw dashed. Client associations stay dashed.
  { selector: '.link--wired', style: { 'line-style': 'solid', 'line-color': '#22d3ee', width: 3 } },
  { selector: '.link--wireless', style: { 'line-style': 'dashed' } },
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
      <span><i class="dot dot--multiap"></i> multi-AP (no 802.11s) ⚠</span>
      <span><i class="dot dot--stale"></i> stale (overdue · last seen)</span>
      <span><i class="dot dot--down"></i> down (onboarded, silent) ✕</span>
      <span><i class="line line--wired"></i> wired (solid · speed)</span>
      <span><i class="line line--wireless"></i> wireless (dashed · RSSI)</span>
      <span><i class="line line--excellent"></i> excellent</span>
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
.dot--multiap {
  background: #f59e0b;
}
.dot--stale {
  background: #64748b;
  opacity: 0.55;
}
.dot--down {
  background: #475569;
  border: 2px dashed #ef4444;
  opacity: 0.6;
}
.line {
  width: 16px;
  height: 0;
  display: inline-block;
  border-top: 3px solid var(--muted);
}
.line--wired {
  border-top-style: solid;
  border-top-color: #22d3ee;
}
.line--wireless {
  border-top-style: dashed;
}
.line--excellent {
  border-top-color: #4ade80;
}
.line--good {
  border-top-color: #86efac;
}
.line--fair {
  border-top-color: #facc15;
}
.line--weak {
  border-top-color: #f87171;
}
</style>
