<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const info = useAsync(async () => {
  const [setup, status, activeHome] = await Promise.all([
    props.client.getSetup(),
    props.client.getStatus(),
    props.client.getActiveHome().catch(() => ''),
  ])
  return { setup, status: status.status, activeHome }
})

onMounted(info.run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Settings</h2>
      <button class="btn" :disabled="info.loading.value" @click="info.run">Refresh</button>
    </header>

    <AsyncSection :loading="info.loading.value" :error="info.error.value">
      <dl v-if="info.data.value" class="detail">
        <dt>Daemon</dt><dd>{{ info.data.value.status }}</dd>
        <dt>This Home</dt><dd>{{ info.data.value.setup.home_name }} <code>({{ info.data.value.setup.home_id }})</code></dd>
        <dt>Active Home</dt><dd>{{ info.data.value.activeHome || '—' }}</dd>
        <dt>Device serial</dt><dd>{{ info.data.value.setup.serial || '—' }}</dd>
        <dt>Node ID</dt><dd><code>{{ info.data.value.setup.node_id }}</code></dd>
        <dt>Onboarding</dt>
        <dd>
          <span :class="['badge', info.data.value.setup.setup_complete ? 'badge--active' : '']">
            {{ info.data.value.setup.setup_complete ? 'complete' : 'not complete' }}
          </span>
        </dd>
      </dl>

      <div class="form__row" style="margin-top: 1rem">
        <RouterLink to="/setup" class="btn">Re-run setup</RouterLink>
        <RouterLink to="/enroll" class="btn">Manage enrollment</RouterLink>
        <RouterLink to="/homes" class="btn">Manage Homes</RouterLink>
      </div>
    </AsyncSection>
  </section>
</template>
