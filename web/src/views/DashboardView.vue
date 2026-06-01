<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const overview = useAsync(async () => {
  const [status, homes, nodes] = await Promise.all([
    props.client.getStatus(),
    props.client.listHomes(),
    props.client.listNodes(),
  ])
  return { status: status.status, homes: homes.length, nodes: nodes.length }
})

const cards = computed(() => {
  const value = overview.data.value
  return [
    { label: 'Daemon', value: value?.status ?? '—' },
    { label: 'Homes', value: value?.homes ?? 0 },
    { label: 'Nodes', value: value?.nodes ?? 0 },
  ]
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
    </AsyncSection>
  </section>
</template>
