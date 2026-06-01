<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
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

// Join-a-home form: enroll this device into another controller.
const join = reactive({ url: '', serial: '' })
const joining = ref(false)
const joinResult = ref<string | null>(null)
const joinError = ref<string | null>(null)

async function joinHome() {
  if (!join.url.trim()) {
    joinError.value = 'Controller URL is required'
    return
  }
  joining.value = true
  joinError.value = null
  joinResult.value = null
  try {
    const res = await props.client.joinHome(join.url.trim(), join.serial.trim() || undefined)
    joinResult.value = `Joined — status: ${res.status}`
    join.url = ''
    join.serial = ''
  } catch (err) {
    joinError.value = err instanceof Error ? err.message : String(err)
  } finally {
    joining.value = false
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

    <form class="form" data-test="join-form" @submit.prevent="joinHome">
      <h3 class="form__title">Join another Home</h3>
      <p class="form__hint">Enroll this device into another controller (it stays the controller of its own Home).</p>
      <div class="form__row">
        <input v-model="join.url" class="input" placeholder="Controller URL (http://host:8080)" />
        <input v-model="join.serial" class="input" placeholder="Serial (optional)" />
        <button class="btn btn--primary" type="submit" :disabled="joining">Join</button>
      </div>
      <p v-if="joinResult" class="form__ok">{{ joinResult }}</p>
      <p v-if="joinError" class="form__error" role="alert">{{ joinError }}</p>
    </form>
  </section>
</template>
