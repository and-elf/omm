<script setup lang="ts">
import { onMounted } from 'vue'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const { data: homes, error, loading, run } = useAsync(() => props.client.listHomes())

onMounted(run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Homes</h2>
      <button class="btn" :disabled="loading" @click="run">Refresh</button>
    </header>

    <AsyncSection :loading="loading" :error="error" :empty="!homes || homes.length === 0" empty-label="No homes yet — create one to get started.">
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Controller</th>
            <th>Home ID</th>
            <th>Last seen</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="home in homes" :key="home.id" data-test="home-row">
            <td>{{ home.name }}</td>
            <td>{{ home.controller || '—' }}</td>
            <td><code>{{ home.id }}</code></td>
            <td>{{ formatLastSeen(home.last_seen) }}</td>
          </tr>
        </tbody>
      </table>
    </AsyncSection>
  </section>
</template>
