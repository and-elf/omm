<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import type { Home } from '@/types'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const { data: homes, error, loading, run } = useAsync(() => props.client.listHomes())
const activeHome = ref('')

const form = reactive({ name: '', controller: '' })
const submitting = ref(false)
const formError = ref<string | null>(null)

async function refresh() {
  await run()
  try {
    activeHome.value = await props.client.getActiveHome()
  } catch {
    activeHome.value = ''
  }
}

async function createHome() {
  if (!form.name.trim()) {
    formError.value = 'Name is required'
    return
  }
  submitting.value = true
  formError.value = null
  try {
    const id = crypto.randomUUID()
    await props.client.createHome({ id, name: form.name.trim(), controller: form.controller.trim() })
    form.name = ''
    form.controller = ''
    await refresh()
  } catch (err) {
    formError.value = err instanceof Error ? err.message : String(err)
  } finally {
    submitting.value = false
  }
}

async function setActive(id: string) {
  await props.client.setActiveHome(id)
  activeHome.value = id
}

const rowError = ref<string | null>(null)

// The daemon refuses to delete the active home (409); the button is disabled
// for it, and we confirm first since deletion also drops the home's profile.
async function removeHome(home: Home) {
  if (!window.confirm(`Delete home "${home.name}"? This also removes its profile and cannot be undone.`)) {
    return
  }
  rowError.value = null
  try {
    await props.client.deleteHome(home.id)
    await refresh()
  } catch (err) {
    rowError.value = err instanceof Error ? err.message : String(err)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Homes</h2>
      <button class="btn" :disabled="loading" @click="refresh">Refresh</button>
    </header>

    <form class="form" data-test="create-home" @submit.prevent="createHome">
      <h3 class="form__title">Create a new Home</h3>
      <div class="form__row">
        <input v-model="form.name" class="input" placeholder="Home name (e.g. Cottage)" />
        <input v-model="form.controller" class="input" placeholder="Controller id (optional)" />
        <button class="btn btn--primary" type="submit" :disabled="submitting">Create</button>
      </div>
      <p v-if="formError" class="form__error" role="alert">{{ formError }}</p>
    </form>

    <AsyncSection
      :loading="loading"
      :error="error"
      :empty="!homes || homes.length === 0"
      empty-label="No homes yet — create one above."
    >
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Controller</th>
            <th>Active</th>
            <th>Last seen</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="home in homes" :key="home.id" data-test="home-row">
            <td><RouterLink :to="`/homes/${home.id}`">{{ home.name }}</RouterLink></td>
            <td>{{ home.controller || '—' }}</td>
            <td>
              <span v-if="home.id === activeHome" class="badge badge--active">active</span>
              <button v-else class="btn btn--small" @click="setActive(home.id)">Set active</button>
            </td>
            <td>{{ formatLastSeen(home.last_seen) }}</td>
            <td class="actions">
              <RouterLink :to="`/homes/${home.id}`" class="link">Profile →</RouterLink>
              <button
                class="btn btn--small btn--danger"
                data-test="delete-home"
                :disabled="home.id === activeHome"
                :title="home.id === activeHome
                  ? 'Switch to another home before deleting this one'
                  : 'Delete this home'"
                @click="removeHome(home)"
              >
                Delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="rowError" class="form__error" role="alert">{{ rowError }}</p>
    </AsyncSection>
  </section>
</template>
