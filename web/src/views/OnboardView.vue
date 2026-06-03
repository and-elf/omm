<script setup lang="ts">
import { computed, reactive, ref, shallowRef } from 'vue'

import { api, ApiClient, createRemoteApi } from '@/api/client'
import { useDiscovery } from '@/composables/useDiscovery'
import { useOnboarding, type OnboardStep, type UseOnboarding } from '@/composables/useOnboarding'
import { getNative } from '@/native'
import type { SetupCredentials } from '@/native'

// `client` reaches the controller (for discovery + adoption); `createClient`
// builds a client for a device over the LAN (the node's setup AP). Both are
// injectable for tests.
const props = withDefaults(
  defineProps<{
    client?: ApiClient
    createClient?: (baseUrl: string) => ApiClient
  }>(),
  {
    client: () => api,
    createClient: () => (baseUrl: string) => createRemoteApi(baseUrl),
  },
)

// Phase 1 — choose the target Home.
const { controllers, source, scanning, error: discoverError, discover } = useDiscovery(props.client)
const controllerUrl = ref('')
const manualUrl = ref('')

function chooseHome(url: string) {
  if (url.trim()) controllerUrl.value = url.trim()
}

// Optional manual setup credentials (used when no QR scanner is available).
const manual = reactive({ enabled: false, ssid: '', password: '', serial: '' })
const qrAvailable = getNative().qr.isAvailable()

// Phase 2 — run onboarding. shallowRef so the composable's inner refs are not
// unwrapped by reactive(), keeping `onboarding.<field>.value` valid in template.
const onboarding = shallowRef<UseOnboarding | null>(null)

function start() {
  if (!controllerUrl.value) return
  const credentials: SetupCredentials | undefined =
    manual.enabled && manual.ssid.trim()
      ? {
          ssid: manual.ssid.trim(),
          password: manual.password.trim() || undefined,
          serial: manual.serial.trim() || undefined,
        }
      : undefined

  const ob = useOnboarding({
    controllerUrl: controllerUrl.value,
    credentials,
    controllerClient: props.client,
    createClient: props.createClient,
    native: getNative(),
  })
  onboarding.value = ob
  void ob.run()
}

function reset() {
  onboarding.value = null
  controllerUrl.value = ''
}

const STEPS: { key: OnboardStep; label: string }[] = [
  { key: 'scan', label: 'Read the device label' },
  { key: 'joinWifi', label: 'Join the node’s setup WiFi' },
  { key: 'connectNode', label: 'Connect to the node' },
  { key: 'enroll', label: 'Request enrollment' },
  { key: 'adopt', label: 'Confirm adoption' },
]
const order: OnboardStep[] = ['idle', 'scan', 'joinWifi', 'connectNode', 'enroll', 'adopt', 'done']

function stepState(key: OnboardStep): 'done' | 'active' | 'error' | 'pending' {
  const ob = onboarding.value
  if (!ob) return 'pending'
  const current = order.indexOf(ob.step.value)
  const mine = order.indexOf(key)
  if (ob.error.value && ob.step.value === key) return 'error'
  if (ob.step.value === 'done' || current > mine) return 'done'
  if (ob.step.value === key) return 'active'
  return 'pending'
}

const finished = computed(() => onboarding.value?.step.value === 'done')
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Add a node</h2>
    </header>

    <!-- Phase 1: choose the Home -->
    <div v-if="!onboarding" class="form" data-test="choose-home">
      <h3 class="form__title">1. Choose the Home to add the node to</h3>
      <p class="form__hint">
        Pick the controller this node should join. Discover it on your network, or enter its URL.
      </p>
      <div class="form__row">
        <button class="btn btn--primary" :disabled="scanning" @click="discover">
          {{ scanning ? 'Scanning…' : 'Discover Homes' }}
        </button>
      </div>
      <p v-if="discoverError" class="form__error" role="alert">{{ discoverError }}</p>
      <p v-if="controllers && controllers.length && source" class="form__hint" data-test="discovery-source">
        Found via {{ source === 'mdns' ? 'mDNS (on-network)' : 'the daemon’s scan' }}.
      </p>
      <table v-if="controllers && controllers.length" class="table" style="margin-top: 0.5rem">
        <tbody>
          <tr v-for="c in controllers" :key="c.home_id" data-test="home-row">
            <td>{{ c.name || c.home_id }}</td>
            <td><code>{{ c.api }}</code></td>
            <td>
              <button class="btn btn--small btn--primary" @click="chooseHome(c.api)">Select</button>
            </td>
          </tr>
        </tbody>
      </table>

      <div class="form__row" style="margin-top: 0.75rem">
        <input v-model="manualUrl" class="input" placeholder="http://controller:8080" />
        <button class="btn" @click="chooseHome(manualUrl)">Use this URL</button>
      </div>

      <details class="form__row" style="margin-top: 0.75rem">
        <summary>Enter setup details manually{{ qrAvailable ? ' (otherwise scanned from the label)' : '' }}</summary>
        <label class="field"><span>Setup SSID</span>
          <input v-model="manual.ssid" class="input" placeholder="OMM-Setup-xxxx" @input="manual.enabled = true" />
        </label>
        <label class="field"><span>Password (if any)</span>
          <input v-model="manual.password" class="input" @input="manual.enabled = true" />
        </label>
      </details>
    </div>

    <!-- Phase 2: run -->
    <div v-else class="form" data-test="run">
      <h3 class="form__title">Adding node to <code>{{ controllerUrl }}</code></h3>

      <ol class="steps">
        <li v-for="s in STEPS" :key="s.key" :data-state="stepState(s.key)" :data-test="`step-${s.key}`">
          {{ s.label }}
        </li>
      </ol>

      <p v-if="onboarding.error.value" class="form__error" role="alert" data-test="onboard-error">
        {{ onboarding.error.value }}
      </p>
      <p v-if="onboarding.note.value" class="form__hint" data-test="onboard-note">{{ onboarding.note.value }}</p>

      <p v-if="finished && onboarding.adopted.value" class="form__ok" data-test="onboard-done">
        Done — node {{ onboarding.nodeId.value }} joined and is active.
      </p>
      <p v-else-if="finished && !onboarding.error.value" class="form__ok" data-test="onboard-pending">
        Enrollment requested for node {{ onboarding.nodeId.value }}.
      </p>

      <div class="form__row" style="margin-top: 0.75rem">
        <button v-if="finished || onboarding.error.value" class="btn" @click="reset">Add another</button>
      </div>
    </div>

    <div v-if="!onboarding && controllerUrl" class="form__row" data-test="ready">
      <button class="btn btn--primary" @click="start">Start — add a node to {{ controllerUrl }}</button>
    </div>
  </section>
</template>

<style scoped>
.steps {
  list-style: none;
  padding: 0;
  margin: 0.5rem 0;
  display: grid;
  gap: 0.4rem;
}
.steps li {
  padding: 0.4rem 0.6rem;
  border-radius: 6px;
  background: var(--surface, #f4f4f5);
}
.steps li[data-state='active'] {
  font-weight: 600;
}
.steps li[data-state='done']::before {
  content: '✓ ';
  color: #16a34a;
}
.steps li[data-state='error']::before {
  content: '✗ ';
  color: #dc2626;
}
</style>
