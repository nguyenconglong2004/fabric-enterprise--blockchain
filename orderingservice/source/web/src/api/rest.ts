const BASE = '/api'

export interface ConfigPatch {
  heartbeat_interval_ms?: number
  heartbeat_timeout_ms?: number
  detection_timeout_ms?: number
  auto_propose_interval_ms?: number
  auto_propose_block_size?: number
  sync_discovery_window_ms?: number
  sync_fetch_timeout_ms?: number
  sync_shard_size?: number
}

export interface NodeInfo {
  port: number
  peerID: string
  address: string
  priority: number
  state: string
  term: number
  alive: boolean
}

export const api = {
  createNetwork: (port: number, config?: ConfigPatch) =>
    fetch(`${BASE}/network`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ port, config }),
    }).then(r => { if (!r.ok) return r.text().then(t => { throw new Error(t) }); return r.json() as Promise<NodeInfo> }),

  listNodes: () =>
    fetch(`${BASE}/nodes`).then(r => r.json() as Promise<NodeInfo[]>),

  addNode: (port: number, config?: ConfigPatch) =>
    fetch(`${BASE}/nodes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ port, config }),
    }).then(r => { if (!r.ok) return r.text().then(t => { throw new Error(t) }); return r.json() as Promise<NodeInfo> }),

  removeNode: (port: number) =>
    fetch(`${BASE}/nodes/${port}`, { method: 'DELETE' }),

  execCmd: (port: number, cmd: string) =>
    fetch(`${BASE}/nodes/${port}/cmd`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cmd }),
    }).then(r => r.json() as Promise<{ output: string }>),

  updateConfig: (port: number, patch: ConfigPatch) =>
    fetch(`${BASE}/nodes/${port}/config`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    }).then(r => r.json()),
}
