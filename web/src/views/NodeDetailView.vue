<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const route = useRoute()
const nodeId = String(route.params.id)
const { data: node, error, loading, run } = useAsync(() => props.client.getNode(nodeId))

onMounted(run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Node</h2>
      <RouterLink to="/nodes" class="btn">← Nodes</RouterLink>
    </header>

    <AsyncSection :loading="loading" :error="error" :empty="!node">
      <dl v-if="node" class="detail">
        <dt>Serial</dt><dd>{{ node.serial }}</dd>
        <dt>Node ID</dt><dd><code>{{ node.id }}</code></dd>
        <dt>Current home</dt><dd>{{ node.current_home || '—' }}</dd>
        <dt>Trusted homes</dt>
        <dd>
          <span v-if="!node.trusted_homes || node.trusted_homes.length === 0">—</span>
          <span v-for="h in node.trusted_homes" v-else :key="h" class="badge">{{ h }}</span>
        </dd>
        <dt>Last seen</dt><dd>{{ formatLastSeen(node.last_seen) }}</dd>
      </dl>
    </AsyncSection>
  </section>
</template>
