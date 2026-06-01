<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
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
            <td><RouterLink :to="`/homes/${home.id}`" class="link">Profile →</RouterLink></td>
          </tr>
        </tbody>
      </table>
    </AsyncSection>
  </section>
</template>
