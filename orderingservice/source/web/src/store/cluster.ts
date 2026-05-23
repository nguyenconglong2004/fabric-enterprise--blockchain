import { create } from 'zustand'

export type NodeState = 'Follower' | 'Leader' | 'ClaimingLeader' | 'Syncing' | 'Offline'

export interface NodeData {
  port: number
  peerID: string
  address: string
  state: NodeState
  term: number
  lastHbAt: number     // epoch ms, when last HB was received
  hbTimeoutMs: number  // current config value
}

export interface HbBeam {
  id: string
  fromPort: number
  toPort: number
  ts: number
}

interface ClusterStore {
  connected: boolean
  nodes: Record<number, NodeData>
  beams: HbBeam[]
  selectedPort: number | null
  globalTerm: number
  logs: Record<number, string[]>       // port → last 200 lines
  cmdOutputs: Record<number, string[]> // port → cmd outputs (for terminal)

  setConnected: (v: boolean) => void
  setSelectedPort: (port: number | null) => void
  setNodeList: (nodes: import('../api/rest').NodeInfo[]) => void
  appendLog: (port: number, line: string) => void
  handleEvent: (ev: { type: string; data: Record<string, unknown> }) => void
}

const MAX_LOG_LINES = 300
const BEAM_TTL = 800 // ms

export const useClusterStore = create<ClusterStore>((set, get) => ({
  connected: false,
  nodes: {},
  beams: [],
  selectedPort: null,
  globalTerm: 0,
  logs: {},
  cmdOutputs: {},

  setConnected: v => set({ connected: v }),
  setSelectedPort: port => set({ selectedPort: port }),

  setNodeList: nodes => {
    const existing = get().nodes
    const updated: Record<number, NodeData> = {}
    for (const n of nodes) {
      updated[n.port] = {
        port: n.port,
        peerID: n.peerID,
        address: n.address,
        state: (n.state as NodeState) ?? 'Follower',
        term: n.term,
        lastHbAt: existing[n.port]?.lastHbAt ?? Date.now(),
        hbTimeoutMs: existing[n.port]?.hbTimeoutMs ?? 5000,
      }
    }
    set({ nodes: updated })
  },

  appendLog: (port, line) => set(s => {
    const prev = s.logs[port] ?? []
    const next = prev.length >= MAX_LOG_LINES
      ? [...prev.slice(prev.length - MAX_LOG_LINES + 1), line]
      : [...prev, line]
    return { logs: { ...s.logs, [port]: next } }
  }),

  handleEvent: ev => {
    const d = ev.data as Record<string, unknown>
    const port = d.port as number | undefined

    set(s => {
      switch (ev.type) {
        case 'node-added': {
          const p = d.port as number
          return {
            nodes: {
              ...s.nodes,
              [p]: {
                port: p,
                peerID: (d.peerID as string) ?? '',
                address: (d.address as string) ?? '',
                state: 'Follower' as NodeState,
                term: 0,
                lastHbAt: Date.now(),
                hbTimeoutMs: 5000,
              },
            },
          }
        }

        case 'node-removed': {
          const { [d.port as number]: _removed, ...rest } = s.nodes
          return { nodes: rest }
        }

        case 'state-changed': {
          if (!port || !s.nodes[port]) return {}
          return {
            nodes: {
              ...s.nodes,
              [port]: { ...s.nodes[port], state: d.to as NodeState },
            },
          }
        }

        case 'term-changed': {
          if (!port || !s.nodes[port]) return {}
          const term = d.term as number
          return {
            globalTerm: Math.max(s.globalTerm, term),
            nodes: { ...s.nodes, [port]: { ...s.nodes[port], term } },
          }
        }

        case 'became-leader': {
          if (!port || !s.nodes[port]) return {}
          const term = d.term as number
          return {
            globalTerm: Math.max(s.globalTerm, term),
            nodes: { ...s.nodes, [port]: { ...s.nodes[port], state: 'Leader', term } },
          }
        }

        case 'heartbeat-received': {
          if (!port || !s.nodes[port]) return {}
          return {
            nodes: { ...s.nodes, [port]: { ...s.nodes[port], lastHbAt: Date.now() } },
          }
        }

        case 'heartbeat-sent': {
          const fromPort = d.fromPort as number
          const toPort = d.toPort as number
          const beam: HbBeam = { id: `${fromPort}-${toPort}-${Date.now()}`, fromPort, toPort, ts: Date.now() }
          const beams = [...s.beams.filter(b => Date.now() - b.ts < BEAM_TTL), beam]
          return { beams }
        }

        case 'log': {
          const p = d.port as number
          const line = d.line as string
          const prev = s.logs[p] ?? []
          const next = prev.length >= MAX_LOG_LINES
            ? [...prev.slice(1), line]
            : [...prev, line]
          return { logs: { ...s.logs, [p]: next } }
        }

        case 'cmd-output': {
          const p = d.port as number
          const output = d.output as string
          const prev = s.cmdOutputs[p] ?? []
          return { cmdOutputs: { ...s.cmdOutputs, [p]: [...prev, output] } }
        }

        default:
          return {}
      }
    })
  },
}))
