<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import { api, ApiClient } from '@/api/client'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})
const router = useRouter()

type Choice = 'create' | 'join' | 'advanced' | null
const choice = ref<Choice>(null)
const homeID = ref('')
const busy = ref(false)
const error = ref<string | null>(null)

const create = reactive({ name: '' })
const join = reactive({ url: '', serial: '' })

async function loadSelf() {
  try {
    const s = await props.client.getSetup()
    homeID.value = s.home_id
    create.name = s.home_name
  } catch {
    // best-effort; the wizard still works for join/advanced
  }
}

async function run(fn: () => Promise<void>) {
  busy.value = true
  error.value = null
  try {
    await fn()
    await props.client.completeSetup()
    router.push('/')
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

function createHome() {
  if (!create.name.trim()) {
    error.value = 'Please name your Home'
    return
  }
  run(async () => {
    await props.client.updateHome(homeID.value, { name: create.name.trim() })
    await props.client.setActiveHome(homeID.value)
  })
}

function joinHome() {
  if (!join.url.trim()) {
    error.value = 'Controller URL is required'
    return
  }
  run(async () => {
    await props.client.joinHome(join.url.trim(), join.serial.trim() || undefined)
  })
}

function skip() {
  run(async () => {})
}

onMounted(loadSelf)
</script>

<template>
  <div class="setup">
    <div class="setup__card">
      <h1 class="setup__brand">OMM</h1>
      <p class="setup__lead">Welcome — let's set up this device.</p>

      <div v-if="!choice" class="setup__choices">
        <button class="choice" data-test="choice-create" @click="choice = 'create'">
          <strong>Create a new Home</strong>
          <span>Make this device the controller of a new mesh.</span>
        </button>
        <button class="choice" data-test="choice-join" @click="choice = 'join'">
          <strong>Join an existing Home</strong>
          <span>Enroll this device into another controller.</span>
        </button>
        <button class="choice" data-test="choice-advanced" @click="choice = 'advanced'">
          <strong>Advanced / skip</strong>
          <span>Go straight to the dashboard.</span>
        </button>
      </div>

      <form v-else-if="choice === 'create'" class="setup__form" data-test="form-create" @submit.prevent="createHome">
        <label class="field"><span>Home name</span>
          <input v-model="create.name" class="input" placeholder="e.g. Main House" autofocus />
        </label>
        <div class="form__row">
          <button class="btn btn--primary" type="submit" :disabled="busy">Create Home</button>
          <button class="btn" type="button" @click="choice = null">Back</button>
        </div>
      </form>

      <form v-else-if="choice === 'join'" class="setup__form" data-test="form-join" @submit.prevent="joinHome">
        <label class="field"><span>Controller URL</span>
          <input v-model="join.url" class="input" placeholder="http://host:8080" autofocus />
        </label>
        <label class="field"><span>Serial (optional)</span>
          <input v-model="join.serial" class="input" placeholder="device serial" />
        </label>
        <div class="form__row">
          <button class="btn btn--primary" type="submit" :disabled="busy">Join Home</button>
          <button class="btn" type="button" @click="choice = null">Back</button>
        </div>
      </form>

      <div v-else class="setup__form" data-test="form-advanced">
        <p>Skip onboarding and manage everything from the dashboard.</p>
        <div class="form__row">
          <button class="btn btn--primary" :disabled="busy" @click="skip">Continue to dashboard</button>
          <button class="btn" type="button" @click="choice = null">Back</button>
        </div>
      </div>

      <p v-if="error" class="form__error" role="alert">{{ error }}</p>
    </div>
  </div>
</template>

<style scoped>
.setup {
  display: flex;
  justify-content: center;
  padding: 3rem 1rem;
}
.setup__card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 1rem;
  padding: 2rem;
  max-width: 460px;
  width: 100%;
}
.setup__brand {
  color: var(--accent);
  letter-spacing: 0.1em;
  margin: 0;
}
.setup__lead {
  color: var(--muted);
  margin: 0.25rem 0 1.5rem;
}
.setup__choices {
  display: grid;
  gap: 0.75rem;
}
.choice {
  text-align: left;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  padding: 1rem;
  color: var(--text);
  cursor: pointer;
  display: grid;
  gap: 0.25rem;
}
.choice:hover {
  border-color: var(--accent);
}
.choice span {
  color: var(--muted);
  font-size: 0.85rem;
}
</style>
