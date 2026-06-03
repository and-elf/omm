<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { useDiscovery } from '@/composables/useDiscovery'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const { data: pending, error, loading, run } = useAsync(() => props.client.listPendingEnrollments())
const actionError = ref<string | null>(null)

async function approve(nodeId: string) {
  await act(() => props.client.adoptNode(nodeId))
}
async function reject(nodeId: string) {
  await act(() => props.client.rejectNode(nodeId))
}
async function act(fn: () => Promise<unknown>) {
  actionError.value = null
  try {
    await fn()
    await run()
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : String(err)
  }
}

// Discover nearby Homes to join (instead of typing a URL). useDiscovery prefers
// native mDNS when the app runs in a native shell and falls back to the daemon's
// /scan; `source` records which path answered.
const {
  controllers: found,
  source: discoverySource,
  scanning,
  error: scanError,
  discover,
} = useDiscovery(props.client)
const joiningApi = ref<string | null>(null)
const joinMessage = ref<string | null>(null)
const joinError = ref<string | null>(null)
const showManual = ref(false)
const manualUrl = ref('')

async function scan() {
  joinError.value = null
  joinMessage.value = null
  await discover()
}

async function join(controllerUrl: string) {
  if (!controllerUrl.trim()) {
    joinError.value = 'Controller URL is required'
    return
  }
  joiningApi.value = controllerUrl
  joinError.value = null
  joinMessage.value = null
  try {
    const res = await props.client.joinHome(controllerUrl.trim())
    joinMessage.value = `Joined — status: ${res.status}`
  } catch (err) {
    joinError.value = err instanceof Error ? err.message : String(err)
  } finally {
    joiningApi.value = null
  }
}

onMounted(run)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Enrollment</h2>
      <button class="btn" :disabled="loading" @click="run">Refresh</button>
    </header>

    <h3 class="form__title">Devices awaiting approval</h3>
    <p v-if="actionError" class="form__error" role="alert">{{ actionError }}</p>
    <AsyncSection
      :loading="loading"
      :error="error"
      :empty="!pending || pending.length === 0"
      empty-label="No devices are waiting for approval."
    >
      <table class="table">
        <thead>
          <tr>
            <th>Serial</th>
            <th>Node ID</th>
            <th>Requested</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in pending" :key="e.id" data-test="pending-row">
            <td>{{ e.serial || '—' }}</td>
            <td><code>{{ e.node_id.slice(0, 16) }}…</code></td>
            <td>{{ formatLastSeen(e.created_at) }}</td>
            <td class="actions">
              <button class="btn btn--primary btn--small" @click="approve(e.node_id)">Approve</button>
              <button class="btn btn--small" @click="reject(e.node_id)">Reject</button>
            </td>
          </tr>
        </tbody>
      </table>
    </AsyncSection>

    <div class="form" data-test="join-section">
      <h3 class="form__title">Join another Home</h3>
      <p class="form__hint">
        Enrol this device into a nearby controller (it stays the controller of its own Home).
      </p>
      <div class="form__row">
        <button class="btn btn--primary" :disabled="scanning" @click="scan">
          {{ scanning ? 'Scanning…' : 'Scan for Homes' }}
        </button>
      </div>

      <p v-if="scanError" class="form__error" role="alert">{{ scanError }}</p>

      <p v-if="found && found.length && discoverySource" class="form__hint" data-test="discovery-source">
        Found via {{ discoverySource === 'mdns' ? 'mDNS (on-network)' : 'the daemon’s scan' }}.
      </p>
      <table v-if="found && found.length" class="table" style="margin-top: 0.75rem">
        <thead>
          <tr><th>Home</th><th>Controller</th><th></th></tr>
        </thead>
        <tbody>
          <tr v-for="c in found" :key="c.home_id" data-test="found-row">
            <td>{{ c.name || c.home_id }}</td>
            <td><code>{{ c.controller_id || '—' }}</code></td>
            <td>
              <button class="btn btn--small btn--primary" :disabled="joiningApi === c.api" @click="join(c.api)">
                {{ joiningApi === c.api ? 'Joining…' : 'Join' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else-if="found && !found.length" class="async__state">
        No Homes found nearby. Make sure a controller is reachable, or enter its URL manually.
      </p>

      <p class="form__row" style="margin-top: 0.75rem">
        <button class="btn btn--small" @click="showManual = !showManual">
          {{ showManual ? 'Hide manual entry' : 'Enter a URL manually' }}
        </button>
      </p>
      <div v-if="showManual" class="form__row" data-test="manual-join">
        <input v-model="manualUrl" class="input" placeholder="http://host:8080" />
        <button class="btn" :disabled="joiningApi === manualUrl" @click="join(manualUrl)">Join</button>
      </div>

      <p v-if="joinMessage" class="form__ok">{{ joinMessage }}</p>
      <p v-if="joinError" class="form__error" role="alert">{{ joinError }}</p>
    </div>
  </section>
</template>
