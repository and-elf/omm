<script setup lang="ts">
import { onMounted } from 'vue'

import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const { data: nodes, error, loading, run } = useAsync(() => props.client.listNodes())

onMounted(run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Nodes</h2>
      <button class="btn" :disabled="loading" @click="run">Refresh</button>
    </header>

    <AsyncSection
      :loading="loading"
      :error="error"
      :empty="!nodes || nodes.length === 0"
      empty-label="No nodes enrolled yet."
    >
      <table class="table">
        <thead>
          <tr>
            <th>Serial</th>
            <th>Current home</th>
            <th>Trusted homes</th>
            <th>Last seen</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="node in nodes" :key="node.id" data-test="node-row">
            <td><RouterLink :to="`/nodes/${node.id}`">{{ node.serial }}</RouterLink></td>
            <td>{{ node.current_home || '—' }}</td>
            <td>{{ (node.trusted_homes ?? []).length }}</td>
            <td>{{ formatLastSeen(node.last_seen) }}</td>
          </tr>
        </tbody>
      </table>
    </AsyncSection>
  </section>
</template>
