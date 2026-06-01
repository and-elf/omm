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

export interface Profile {
  home_id: string
  node_name: string
  mesh_ssid: string
  mesh_key: string
  vlans: string[] | null
}

export interface Status {
  status: string
}

export interface ActiveHome {
  home_id: string
}

export interface Setup {
  setup_complete: boolean
  node_id: string
  serial: string
  home_id: string
  home_name: string
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
