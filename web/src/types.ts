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
