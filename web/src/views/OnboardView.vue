<script setup lang="ts">
import { computed, reactive, ref, shallowRef } from 'vue'

import { api, ApiClient, createRemoteApi } from '@/api/client'
import { connectController, type ConnectOptions } from '@/api/controller'
import { useDiscovery } from '@/composables/useDiscovery'
import {
  NODE_SETUP_URL,
  useOnboarding,
  type OnboardStep,
  type UseOnboarding,
} from '@/composables/useOnboarding'
import { getNative } from '@/native'
import type { SetupCredentials, WifiNetwork } from '@/native'

// `client` reaches the controller (for discovery + adoption); `createClient`
// builds a client for a device over the LAN (the node's setup AP); `connect`
// builds an authenticated controller client (LuCI login) for split-mode
// controllers. All injectable for tests.
const props = withDefaults(
  defineProps<{
    client?: ApiClient
    createClient?: (baseUrl: string) => ApiClient
    connect?: (baseUrl: string, opts: ConnectOptions) => Promise<ApiClient>
  }>(),
  {
    client: () => api,
    // createClient and connect are function-typed props: Vue uses the default as
    // the value itself (it is not called as a factory the way `client` is), so
    // these must BE the function, not a factory returning it.
    createClient: (baseUrl: string) => createRemoteApi(baseUrl),
    connect: connectController,
  },
)

const native = getNative()

// The wizard is three input-light pages (see doc/companion-app.md §13):
//   home   — Page 1: choose the target Home (on the home WiFi) + optional sign-in
//   device — Page 2: identify the new node (after switching to its setup AP)
//   run    — Page 3: auto-progress to adopted; a confirmation screen, no input
type Page = 'home' | 'device' | 'run'
const page = ref<Page>('home')

// ── Page 1: choose the target Home ──────────────────────────────────────────
const { controllers, source, scanning, error: discoverError, discover } = useDiscovery(props.client)
const controllerUrl = ref('')
const manualUrl = ref('')

function chooseHome(url: string) {
  if (url.trim()) controllerUrl.value = url.trim()
}

// Optional controller sign-in for split-mode controllers (management API bound
// to localhost behind LuCI). Collected here, on the home network; the actual
// LuCI login happens at start(). When credentials are given the adoption client
// is obtained via LuCI session.login; otherwise the same-origin client is used.
const auth = reactive({ enabled: false, mgmtUrl: '', username: '', password: '' })

function toDevicePage() {
  if (!controllerUrl.value) return
  page.value = 'device'
  void scanForSetupAps()
}

// ── Page 2: identify the new node (on its setup AP) ─────────────────────────
// The node's management URL. Defaults to the setup-AP gateway (the device you
// joined over WiFi); override it for a wired node at a known address.
const nodeUrl = ref(NODE_SETUP_URL)
const qrAvailable = native.qr.isAvailable()

// Android-only convenience: list nearby setup APs so the operator can tap one
// instead of typing the SSID. iOS/web have no scan API (scanNetworks absent),
// so this is hidden and the QR/manual path is used instead.
const canScan = typeof native.wifi.scanNetworks === 'function'
const setupAps = ref<WifiNetwork[]>([])
const scanError = ref<string | null>(null)

async function scanForSetupAps() {
  if (!native.wifi.scanNetworks) return
  scanError.value = null
  try {
    const nets = await native.wifi.scanNetworks()
    setupAps.value = nets.filter((n) => n.ssid.startsWith('OMM-Setup-'))
  } catch (err) {
    scanError.value = err instanceof Error ? err.message : String(err)
  }
}

// Setup credentials, from the picker or manual entry. When left empty the run
// scans the label QR instead (native QR scanner).
const manual = reactive({ ssid: '', password: '', serial: '' })

// How the node reaches the controller while enrolling. 'wired' assumes an
// Ethernet uplink (no extra setup). 'wifi' provisions a station uplink on the
// node from the home-WiFi credentials below (POST /setup/uplink).
const uplink = ref<'wired' | 'wifi'>('wired')
const homeWifi = reactive({ ssid: '', password: '' })

function pickSsid(ssid: string) {
  manual.ssid = ssid
}

// ── Page 3: run onboarding. shallowRef so the composable's inner refs are not
// unwrapped by reactive(), keeping `onboarding.<field>.value` valid in template.
const onboarding = shallowRef<UseOnboarding | null>(null)
const connecting = ref(false)
const connectError = ref<string | null>(null)

async function start() {
  if (!controllerUrl.value) return
  connectError.value = null

  const credentials: SetupCredentials | undefined = manual.ssid.trim()
    ? {
        ssid: manual.ssid.trim(),
        password: manual.password.trim() || undefined,
        serial: manual.serial.trim() || undefined,
      }
    : undefined

  // Pick the client used to confirm adoption: an authenticated one when the
  // operator supplied controller credentials, else the same-origin client.
  // Adoption runs in the background once the node has enrolled — there is no
  // operator approve step (see doc/companion-app.md §13).
  let controllerClient: ApiClient = props.client
  if (auth.enabled && auth.username.trim()) {
    connecting.value = true
    try {
      controllerClient = await props.connect(auth.mgmtUrl.trim() || controllerUrl.value, {
        username: auth.username.trim(),
        password: auth.password,
      })
    } catch (err) {
      connectError.value = err instanceof Error ? err.message : String(err)
      return
    } finally {
      connecting.value = false
    }
  }

  const ob = useOnboarding({
    controllerUrl: controllerUrl.value,
    nodeUrl: nodeUrl.value.trim() || NODE_SETUP_URL,
    credentials,
    controllerClient,
    createClient: props.createClient,
    native,
    uplink: uplink.value,
    homeWifi:
      uplink.value === 'wifi'
        ? { ssid: homeWifi.ssid.trim(), password: homeWifi.password.trim() || undefined }
        : undefined,
  })
  onboarding.value = ob
  page.value = 'run'
  void ob.run()
}

function reset() {
  onboarding.value = null
  controllerUrl.value = ''
  manual.ssid = ''
  manual.password = ''
  manual.serial = ''
  setupAps.value = []
  uplink.value = 'wired'
  homeWifi.ssid = ''
  homeWifi.password = ''
  page.value = 'home'
}

// The provisionUplink step is shown only for a WiFi uplink; a wired node skips it.
const STEPS = computed<{ key: OnboardStep; label: string }[]>(() => [
  { key: 'scan', label: 'Read the device label' },
  { key: 'joinWifi', label: 'Join the node’s setup WiFi' },
  { key: 'connectNode', label: 'Connect to the node' },
  ...(uplink.value === 'wifi'
    ? [{ key: 'provisionUplink' as OnboardStep, label: 'Connect the node to your WiFi' }]
    : []),
  { key: 'enroll', label: 'Request enrollment' },
  { key: 'adopt', label: 'Confirm adoption' },
])
const order: OnboardStep[] = [
  'idle',
  'scan',
  'joinWifi',
  'connectNode',
  'provisionUplink',
  'enroll',
  'adopt',
  'done',
]

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

    <!-- Page 1: choose the Home -->
    <div v-if="page === 'home'" class="form" data-test="page-home">
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
              <button class="btn btn--small btn--primary" @click="chooseHome(c.api)">
                {{ controllerUrl === c.api ? 'Selected' : 'Select' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <div class="form__row" style="margin-top: 0.75rem">
        <input v-model="manualUrl" class="input" placeholder="http://controller:8080" />
        <button class="btn" @click="chooseHome(manualUrl)">Use this URL</button>
      </div>

      <details class="form__row" data-test="controller-auth" style="margin-top: 0.5rem">
        <summary>Controller sign-in (for split-mode controllers behind LuCI)</summary>
        <p class="form__hint">
          If the controller’s management API is localhost-bound, sign in to confirm adoption in-app.
        </p>
        <label class="field"><span>Management URL</span>
          <input v-model="auth.mgmtUrl" class="input" :placeholder="controllerUrl || 'http://controller'" @input="auth.enabled = true" />
        </label>
        <label class="field"><span>Username</span>
          <input v-model="auth.username" class="input" data-test="auth-username" placeholder="root" @input="auth.enabled = true" />
        </label>
        <label class="field"><span>Password</span>
          <input v-model="auth.password" type="password" class="input" data-test="auth-password" @input="auth.enabled = true" />
        </label>
      </details>

      <div class="form__row" style="margin-top: 0.75rem">
        <button
          class="btn btn--primary"
          data-test="home-continue"
          :disabled="!controllerUrl"
          @click="toDevicePage"
        >
          Continue
        </button>
        <span v-if="controllerUrl" class="form__hint">Adding to <code>{{ controllerUrl }}</code></span>
      </div>
    </div>

    <!-- Page 2: identify the new node -->
    <div v-else-if="page === 'device'" class="form" data-test="page-device">
      <h3 class="form__title">2. Choose the device to add</h3>
      <p class="form__hint">
        Connect to the new node’s setup WiFi, then identify it. Its setup network is named
        <code>OMM-Setup-…</code> and is printed on the device label.
      </p>

      <!-- Android-only: pick a nearby setup AP. -->
      <div v-if="canScan" class="form__row" data-test="ssid-picker">
        <div class="form__row">
          <button class="btn" @click="scanForSetupAps">Rescan</button>
        </div>
        <p v-if="scanError" class="form__error" role="alert">{{ scanError }}</p>
        <p v-else-if="!setupAps.length" class="form__hint">No <code>OMM-Setup-…</code> networks found yet.</p>
        <table v-else class="table">
          <tbody>
            <tr v-for="n in setupAps" :key="n.ssid" data-test="ssid-row">
              <td><code>{{ n.ssid }}</code></td>
              <td>
                <button
                  class="btn btn--small"
                  :class="manual.ssid === n.ssid ? 'btn--primary' : ''"
                  @click="pickSsid(n.ssid)"
                >
                  {{ manual.ssid === n.ssid ? 'Selected' : 'Select' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- iOS/web (no scan): scan the label QR, or enter the details manually. -->
      <div v-else class="form__row" data-test="manual-ssid">
        <p class="form__hint">
          {{ qrAvailable ? 'The label QR is read automatically when the run starts, or enter it manually:' : 'Enter the setup details from the device label:' }}
        </p>
        <label class="field"><span>Setup SSID</span>
          <input v-model="manual.ssid" class="input" placeholder="OMM-Setup-xxxx" />
        </label>
      </div>

      <label class="field" style="margin-top: 0.5rem"><span>Password (if any)</span>
        <input v-model="manual.password" class="input" />
      </label>
      <label class="field"><span>Serial (optional)</span>
        <input v-model="manual.serial" class="input" placeholder="from the label" />
      </label>

      <label class="field" style="margin-top: 0.5rem">
        <span>Node URL</span>
        <input v-model="nodeUrl" class="input" data-test="node-url" :placeholder="NODE_SETUP_URL" />
        <small class="form__hint">The new node’s address — the setup-AP default, or a wired node’s URL.</small>
      </label>

      <fieldset class="form__row" data-test="uplink" style="margin-top: 0.75rem; border: 0; padding: 0">
        <legend class="form__hint">How does this node reach your network?</legend>
        <label class="field--inline">
          <input type="radio" value="wired" v-model="uplink" data-test="uplink-wired" />
          <span>Wired (Ethernet) — it’s already on the network</span>
        </label>
        <label class="field--inline">
          <input type="radio" value="wifi" v-model="uplink" data-test="uplink-wifi" />
          <span>WiFi — connect it to your home WiFi</span>
        </label>
      </fieldset>

      <div v-if="uplink === 'wifi'" class="form__row" data-test="home-wifi">
        <p class="form__hint">The node joins this network to reach the controller while it enrolls.</p>
        <label class="field"><span>Home WiFi name (SSID)</span>
          <input v-model="homeWifi.ssid" class="input" data-test="home-wifi-ssid" placeholder="HomeNet" />
        </label>
        <label class="field"><span>Home WiFi password</span>
          <input v-model="homeWifi.password" type="password" class="input" data-test="home-wifi-password" />
        </label>
      </div>

      <p v-if="connectError" class="form__error" role="alert" data-test="connect-error">{{ connectError }}</p>

      <div class="form__row" style="margin-top: 0.75rem">
        <button class="btn" @click="page = 'home'">Back</button>
        <button class="btn btn--primary" data-test="device-start" :disabled="connecting" @click="start">
          {{ connecting ? 'Signing in…' : 'Start' }}
        </button>
      </div>
    </div>

    <!-- Page 3: run (confirmation/progress; no input) -->
    <div v-else class="form" data-test="page-run">
      <h3 class="form__title">Adding node to <code>{{ controllerUrl }}</code></h3>

      <ol class="steps">
        <li v-for="s in STEPS" :key="s.key" :data-state="stepState(s.key)" :data-test="`step-${s.key}`">
          {{ s.label }}
        </li>
      </ol>

      <p v-if="onboarding && onboarding.error.value" class="form__error" role="alert" data-test="onboard-error">
        {{ onboarding.error.value }}
      </p>
      <p v-if="onboarding && onboarding.note.value" class="form__hint" data-test="onboard-note">
        {{ onboarding.note.value }}
      </p>

      <p v-if="finished && onboarding && onboarding.adopted.value" class="form__ok" data-test="onboard-done">
        Done — node {{ onboarding.nodeId.value }} joined and is active.
      </p>
      <p v-else-if="finished && onboarding && !onboarding.error.value" class="form__ok" data-test="onboard-pending">
        Enrollment requested for node {{ onboarding.nodeId.value }}.
      </p>

      <div class="form__row" style="margin-top: 0.75rem">
        <button v-if="finished || (onboarding && onboarding.error.value)" class="btn" @click="reset">
          Add another
        </button>
      </div>
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
