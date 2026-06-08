<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import type { Backhaul } from '@/types'
import { backhaulModeLabel } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const overview = useAsync(async () => {
  const [status, homes, nodes] = await Promise.all([
    props.client.getStatus(),
    props.client.listHomes(),
    props.client.listNodes(),
  ])
  return {
    status: status.status,
    backhaul: status.backhaul,
    homes: homes.length,
    nodes: nodes.length,
  }
})

const cards = computed(() => {
  const value = overview.data.value
  return [
    { label: 'Daemon', value: value?.status ?? '—' },
    { label: 'Backhaul', value: backhaulModeLabel(value?.backhaul?.mode) },
    { label: 'Homes', value: value?.homes ?? 0 },
    { label: 'Nodes', value: value?.nodes ?? 0 },
  ]
})

// Shown only when 802.11s was configured but degraded to multi-AP, carrying the
// daemon's reason and remediation so the operator can fix it.
const degraded = computed<Backhaul | null>(() => {
  const bh = overview.data.value?.backhaul
  return bh && bh.mode === 'multi_ap' && bh.reason ? bh : null
})

onMounted(overview.run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Dashboard</h2>
      <button class="btn" :disabled="overview.loading.value" @click="overview.run">Refresh</button>
    </header>

    <AsyncSection :loading="overview.loading.value" :error="overview.error.value">
      <div class="cards">
        <article v-for="card in cards" :key="card.label" class="card">
          <p class="card__value">{{ card.value }}</p>
          <p class="card__label">{{ card.label }}</p>
        </article>
      </div>

      <div v-if="degraded" class="notice notice--warn" role="alert">
        <p class="notice__title">802.11s mesh unavailable — running as wired multi-AP</p>
        <p class="notice__body">{{ degraded.reason }}</p>
        <p v-if="degraded.remediation" class="notice__hint">Fix: {{ degraded.remediation }}</p>
      </div>
    </AsyncSection>
  </section>
</template>

<style scoped>
.notice {
  margin-top: 1rem;
  padding: 0.75rem 1rem;
  border-radius: 6px;
  border: 1px solid transparent;
}
.notice--warn {
  background: #fff7e6;
  border-color: #f0c36d;
  color: #7a5200;
}
.notice__title {
  font-weight: 600;
  margin: 0;
}
.notice__body {
  margin: 0.25rem 0 0;
}
.notice__hint {
  margin: 0.25rem 0 0;
  font-family: monospace;
  font-size: 0.9em;
}
</style>
