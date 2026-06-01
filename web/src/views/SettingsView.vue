<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const info = useAsync(async () => {
  const [setup, status, selection] = await Promise.all([
    props.client.getSetup(),
    props.client.getStatus(),
    props.client.getHomeSelection().catch(() => null),
  ])
  return { setup, status: status.status, selection }
})

const actionError = ref<string | null>(null)

async function setActive(homeId: string) {
  actionError.value = null
  try {
    await props.client.setActiveHome(homeId)
    await info.run()
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : String(err)
  }
}

function signalLabel(signal: number): string {
  return signal === 0 ? '—' : `${signal} dBm`
}

onMounted(info.run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Settings</h2>
      <button class="btn" :disabled="info.loading.value" @click="info.run">Refresh</button>
    </header>

    <AsyncSection :loading="info.loading.value" :error="info.error.value">
      <template v-if="info.data.value">
        <dl class="detail">
          <dt>Daemon</dt><dd>{{ info.data.value.status }}</dd>
          <dt>This Home</dt>
          <dd>{{ info.data.value.setup.home_name }} <code>({{ info.data.value.setup.home_id }})</code></dd>
          <dt>Active Home</dt><dd>{{ info.data.value.selection?.active_home_id || '—' }}</dd>
          <dt>Device serial</dt><dd>{{ info.data.value.setup.serial || '—' }}</dd>
          <dt>Node ID</dt><dd><code>{{ info.data.value.setup.node_id }}</code></dd>
          <dt>Onboarding</dt>
          <dd>
            <span :class="['badge', info.data.value.setup.setup_complete ? 'badge--active' : '']">
              {{ info.data.value.setup.setup_complete ? 'complete' : 'not complete' }}
            </span>
          </dd>
        </dl>

        <h3 class="form__title" style="margin-top: 1.5rem">Home selection</h3>
        <p class="form__hint">
          Recommended:
          <strong>{{ info.data.value.selection?.recommended_home_id || '—' }}</strong>
          (external Home preferred over this device's own; strongest signal wins).
        </p>
        <p v-if="actionError" class="form__error" role="alert">{{ actionError }}</p>

        <table v-if="info.data.value.selection?.candidates?.length" class="table">
          <thead>
            <tr>
              <th>Home</th>
              <th>Signal</th>
              <th>Last active</th>
              <th>Kind</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in info.data.value.selection.candidates" :key="c.home_id" data-test="candidate-row">
              <td>
                {{ c.home_id }}
                <span v-if="c.home_id === info.data.value.selection.recommended_home_id" class="badge badge--active">recommended</span>
              </td>
              <td>{{ signalLabel(c.signal) }}</td>
              <td>{{ formatLastSeen(c.last_active) }}</td>
              <td>{{ c.self_controlled ? 'own' : 'member' }}</td>
              <td>
                <span v-if="c.home_id === info.data.value.selection.active_home_id" class="badge badge--active">active</span>
                <button v-else class="btn btn--small" @click="setActive(c.home_id)">Set active</button>
              </td>
            </tr>
          </tbody>
        </table>
      </template>

      <div class="form__row" style="margin-top: 1rem">
        <RouterLink to="/setup" class="btn">Re-run setup</RouterLink>
        <RouterLink to="/enroll" class="btn">Manage enrollment</RouterLink>
        <RouterLink to="/homes" class="btn">Manage Homes</RouterLink>
      </div>
    </AsyncSection>
  </section>
</template>
