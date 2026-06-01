<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import { ApiError } from '@/api/client'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const route = useRoute()
const homeId = String(route.params.id)

const homeName = ref('')
const loading = ref(true)
const loadError = ref<string | null>(null)
const saving = ref(false)
const saveMessage = ref<string | null>(null)
const saveError = ref<string | null>(null)

const profile = reactive({ node_name: '', mesh_ssid: '', mesh_key: '', vlans: '' })

async function load() {
  loading.value = true
  loadError.value = null
  try {
    const home = await props.client.getHome(homeId)
    homeName.value = home.name
    try {
      const p = await props.client.getProfile(homeId)
      profile.node_name = p.node_name ?? ''
      profile.mesh_ssid = p.mesh_ssid ?? ''
      profile.mesh_key = p.mesh_key ?? ''
      profile.vlans = (p.vlans ?? []).join(', ')
    } catch (err) {
      // A home may not have a profile yet; start with a blank form.
      if (!(err instanceof ApiError) || err.status !== 404) {
        throw err
      }
    }
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  saveMessage.value = null
  saveError.value = null
  try {
    await props.client.saveProfile(homeId, {
      node_name: profile.node_name.trim(),
      mesh_ssid: profile.mesh_ssid.trim(),
      mesh_key: profile.mesh_key,
      vlans: profile.vlans
        .split(',')
        .map((v) => v.trim())
        .filter(Boolean),
    })
    saveMessage.value = 'Profile saved'
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function setActive() {
  await props.client.setActiveHome(homeId)
  saveMessage.value = 'This Home is now active'
}

onMounted(load)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>{{ homeName || 'Home' }}</h2>
      <RouterLink to="/homes" class="btn">← Homes</RouterLink>
    </header>

    <p v-if="loading" class="async__state">Loading…</p>
    <p v-else-if="loadError" class="async__state async__state--error" role="alert">{{ loadError }}</p>

    <form v-else class="form" data-test="profile-form" @submit.prevent="save">
      <h3 class="form__title">Profile</h3>
      <label class="field"><span>Node name</span>
        <input v-model="profile.node_name" class="input" placeholder="e.g. Garage" />
      </label>
      <label class="field"><span>Mesh SSID</span>
        <input v-model="profile.mesh_ssid" class="input" placeholder="HomeMesh" />
      </label>
      <label class="field"><span>Mesh key</span>
        <input v-model="profile.mesh_key" class="input" type="password" placeholder="mesh passphrase" />
      </label>
      <label class="field"><span>VLANs</span>
        <input v-model="profile.vlans" class="input" placeholder="comma-separated, e.g. 10, 20" />
      </label>
      <div class="form__row">
        <button class="btn btn--primary" type="submit" :disabled="saving">Save profile</button>
        <button class="btn" type="button" @click="setActive">Set as active Home</button>
      </div>
      <p v-if="saveMessage" class="form__ok">{{ saveMessage }}</p>
      <p v-if="saveError" class="form__error" role="alert">{{ saveError }}</p>
    </form>
  </section>
</template>
