// Domain types mirroring the meshd JSON API (see internal/models/models.go).

export interface Home {
  id: string
  name: string
  controller: string
  certificate: string | null
  last_seen: number
}

export interface Node {
  id: string
  serial: string
  current_home: string
  trusted_homes: string[] | null
  last_seen: number
}

// Selectable Wi-Fi bands. The empty value means "let the daemon pick its
// default radio". Values match OpenWrt's wifi-device `band` option.
export const BAND_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: '', label: 'Default' },
  { value: '2g', label: '2.4 GHz' },
  { value: '5g', label: '5 GHz' },
  { value: '6g', label: '6 GHz' },
]

export interface Profile {
  home_id: string
  node_name: string
  mesh_ssid: string
  mesh_key: string
  // Optional client-AP overrides. When omitted, the controller broadcasts a
  // client AP using mesh_ssid/mesh_key.
  ap_ssid?: string
  ap_key?: string
  // Band ("2g" | "5g" | "6g") selects the radio by frequency; meshd resolves it
  // to the matching wifi-device. radio is an advanced override naming the
  // wifi-device directly. Precedence: radio, then band, then daemon default.
  band?: string
  radio?: string
  vlans: string[] | null
}

/**
 * Applied wireless-backhaul outcome. mode is '802.11s' when the mesh formed,
 * 'multi_ap' when it degraded to a wired multi-AP, or 'unknown' before a
 * profile is applied. reason/remediation are present only on a degrade.
 */
export interface Backhaul {
  mode: string
  reason?: string
  remediation?: string
}

export interface Status {
  status: string
  backhaul?: Backhaul
}

export interface ActiveHome {
  home_id: string
}

/** A controller (Home) discovered by scanning. */
export interface DiscoveredController {
  home_id: string
  name: string
  controller_id: string
  api: string
}

export interface Setup {
  setup_complete: boolean
  node_id: string
  serial: string
  home_id: string
  home_name: string
}

export interface HomeSelectionCandidate {
  home_id: string
  signal: number
  last_active: number
  self_controlled: boolean
}

export interface HomeSelection {
  recommended_home_id: string
  active_home_id: string
  candidates: HomeSelectionCandidate[] | null
}

export type EnrollmentStatus =
  | 'pending_verification'
  | 'pending_approval'
  | 'approved'
  | 'active'
  | 'rejected'

/** Summary of a pending enrollment, as returned by GET /enroll. */
export interface Enrollment {
  id: string
  node_id: string
  serial: string
  status: EnrollmentStatus
  home_id: string
  created_at: number
}

/** Result of an enrollment action (verify/adopt/reject/join). */
export interface EnrollmentResult {
  status: EnrollmentStatus
  profile?: Profile
}

export interface TopologyNode {
  id: string
  label: string
  role: string
  /** How this node reaches the mesh: 'ethernet' | 'wireless' | 'unknown'. */
  backhaul?: string
  /** Wireless-backhaul mode: '802.11s' | 'multi_ap' | 'unknown'. */
  mesh_mode?: string
}

export interface TopologyLink {
  source: string
  target: string
  tq: number
}

export interface TopologyClient {
  mac: string
  ap: string
  signal: number
  band?: string
  tx_rate?: number
  rx_rate?: number
}

export interface Topology {
  nodes: TopologyNode[] | null
  links: TopologyLink[] | null
  clients: TopologyClient[] | null
}
