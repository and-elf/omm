import { ref, type Ref } from 'vue'

import { ApiClient, createRemoteApi } from '@/api/client'
import { getNative } from '@/native'
import type { NativeBridge, SetupCredentials } from '@/native'

/** The setup-AP gateway an unclaimed node serves its management API on. */
export const NODE_SETUP_URL = 'http://192.168.254.1:8080'

/**
 * The onboarding steps, in order. `scan` reads the device label, `joinWifi`
 * joins its setup AP, `connectNode` reaches the node's management API,
 * `enroll` tells the node to join the chosen Home, and `adopt` confirms the
 * controller has adopted it.
 */
export type OnboardStep = 'idle' | 'scan' | 'joinWifi' | 'connectNode' | 'enroll' | 'adopt' | 'done'

export interface OnboardOptions {
  /** The target Home's controller API URL (chosen before joining the setup AP). */
  controllerUrl: string
  /** Pre-known setup credentials; when absent they are scanned from the label. */
  credentials?: SetupCredentials
  /** The unclaimed node's management URL (defaults to the setup-AP gateway). */
  nodeUrl?: string
  /** Builds a client for a base URL (injectable for tests). */
  createClient?: (baseUrl: string) => ApiClient
  /**
   * A client that can reach the controller, used to confirm/trigger adoption.
   * Omitted when the app cannot reach the controller from the setup AP — the
   * flow then ends at "enrollment requested; approve on the controller".
   */
  controllerClient?: ApiClient
  /** Native bridge (defaults to the active one). */
  native?: NativeBridge
  pollIntervalMs?: number
  pollTimeoutMs?: number
}

export interface UseOnboarding {
  step: Ref<OnboardStep>
  running: Ref<boolean>
  error: Ref<string | null>
  nodeId: Ref<string | null>
  enrollStatus: Ref<string | null>
  adopted: Ref<boolean>
  note: Ref<string | null>
  run: () => Promise<void>
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

/**
 * Drives a new node from its setup label to an adopted member of a Home, per
 * the wired-uplink onboarding model: scan → join WiFi → reach the node →
 * /enroll/join → adopt → confirm. Native steps degrade gracefully when their
 * capability is unavailable (e.g. a browser PWA): WiFi-join is skipped (assume
 * already connected) and a missing QR scanner requires pre-supplied
 * credentials.
 */
export function useOnboarding(opts: OnboardOptions): UseOnboarding {
  const step = ref<OnboardStep>('idle')
  const running = ref(false)
  const error = ref<string | null>(null)
  const nodeId = ref<string | null>(null)
  const enrollStatus = ref<string | null>(null)
  const adopted = ref(false)
  const note = ref<string | null>(null)

  const native = opts.native ?? getNative()
  const createClient = opts.createClient ?? ((base: string) => createRemoteApi(base))
  const nodeUrl = opts.nodeUrl ?? NODE_SETUP_URL
  const pollInterval = opts.pollIntervalMs ?? 1500
  const pollTimeout = opts.pollTimeoutMs ?? 30000

  async function run(): Promise<void> {
    running.value = true
    error.value = null
    note.value = null
    adopted.value = false
    nodeId.value = null
    enrollStatus.value = null

    try {
      // 1. Setup credentials — scanned from the device label, or pre-supplied.
      step.value = 'scan'
      let creds = opts.credentials
      if (!creds) {
        if (!native.qr.isAvailable()) {
          throw new Error('QR scanning is unavailable here; enter the setup details manually')
        }
        creds = await native.qr.scanSetupLabel()
      }

      // 2. Join the node's setup AP (skipped when WiFi control is unavailable).
      step.value = 'joinWifi'
      if (native.wifi.isAvailable()) {
        await native.wifi.joinNetwork(creds.ssid, creds.password)
      } else {
        note.value = 'WiFi join unavailable here — assuming this device is already on the node’s network.'
      }

      // 3. Reach the node's management API and read its identity.
      step.value = 'connectNode'
      const nodeClient = createClient(nodeUrl)
      const setup = await nodeClient.getSetup()
      nodeId.value = setup.node_id

      // 4. Tell the node to enroll into the chosen Home.
      step.value = 'enroll'
      const result = await nodeClient.joinHome(opts.controllerUrl, creds.serial)
      enrollStatus.value = result.status

      // 5. Confirm adoption.
      step.value = 'adopt'
      if (result.status === 'active') {
        adopted.value = true
      } else if (opts.controllerClient) {
        try {
          await confirmAdoption(opts.controllerClient, nodeId.value, pollInterval, pollTimeout)
          adopted.value = true
          enrollStatus.value = 'active'
        } catch (err) {
          // Not fatal: the node is enrolling; the operator can approve it.
          note.value =
            'Could not confirm adoption automatically — approve this node on the controller. ' +
            (err instanceof Error ? err.message : String(err))
        }
      } else {
        note.value = 'Enrollment requested — approve this node on the controller to finish.'
      }

      step.value = 'done'
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    } finally {
      running.value = false
    }
  }

  return { step, running, error, nodeId, enrollStatus, adopted, note, run }
}

// confirmAdoption polls the controller until the node appears in its inventory,
// adopting it once it shows up as a pending enrollment. Resolves when adopted;
// throws on timeout.
async function confirmAdoption(
  controller: ApiClient,
  nodeId: string,
  intervalMs: number,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let adoptSent = false
  for (;;) {
    const nodes = await controller.listNodes()
    if (nodes.some((n) => n.id === nodeId)) return

    if (!adoptSent) {
      const pending = await controller.listPendingEnrollments()
      if (pending.some((e) => e.node_id === nodeId)) {
        await controller.adoptNode(nodeId)
        adoptSent = true
      }
    }

    if (Date.now() >= deadline) {
      throw new Error('timed out waiting for the node to be adopted')
    }
    await sleep(intervalMs)
  }
}
