import { useState, useEffect } from 'react'
import { api, type ConfigPatch } from '../api/rest'
import { useClusterStore } from '../store/cluster'

interface Props { port: number }

export function ConfigPanel({ port }: Props) {
  const node = useClusterStore(s => s.nodes[port])
  const [cfg, setCfg] = useState<ConfigPatch>({
    heartbeat_interval_ms: 2000,
    heartbeat_timeout_ms: 5000,
    auto_propose_interval_ms: 500,
    auto_propose_block_size: 20,
  })
  const [saved, setSaved] = useState(false)

  // Update local hbTimeoutMs in store when config changes
  const nodes = useClusterStore(s => s.nodes)

  const set = (key: keyof ConfigPatch, val: number) =>
    setCfg(prev => ({ ...prev, [key]: val }))

  const apply = async () => {
    try {
      await api.updateConfig(port, cfg)
      // Update heartbeat timeout in store for countdown ring
      if (cfg.heartbeat_timeout_ms) {
        useClusterStore.setState({
          nodes: {
            ...nodes,
            [port]: { ...nodes[port], hbTimeoutMs: cfg.heartbeat_timeout_ms },
          },
        })
      }
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      alert('Config update failed: ' + e)
    }
  }

  if (!node) return null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <Field label="HB Interval (ms)" value={cfg.heartbeat_interval_ms ?? 2000}
        min={100} max={10000} onChange={v => set('heartbeat_interval_ms', v)} />
      <Field label="HB Timeout (ms)" value={cfg.heartbeat_timeout_ms ?? 5000}
        min={500} max={30000} onChange={v => set('heartbeat_timeout_ms', v)} />
      <Field label="Propose Interval (ms)" value={cfg.auto_propose_interval_ms ?? 500}
        min={100} max={5000} onChange={v => set('auto_propose_interval_ms', v)} />
      <Field label="Block Size" value={cfg.auto_propose_block_size ?? 20}
        min={1} max={200} onChange={v => set('auto_propose_block_size', v)} />

      <button onClick={apply} style={{
        background: saved ? '#10B981' : '#3B82F6', color: 'white', border: 'none',
        borderRadius: 4, padding: '6px 12px', cursor: 'pointer',
      }}>
        {saved ? '✓ Applied' : 'Apply'}
      </button>
    </div>
  )
}

function Field({ label, value, min, max, onChange }: {
  label: string; value: number; min: number; max: number; onChange: (v: number) => void
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <label style={{ fontSize: 11, color: '#9CA3AF' }}>{label}: {value}</label>
      <input type="range" min={min} max={max} value={value}
        onChange={e => onChange(Number(e.target.value))}
        style={{ accentColor: '#3B82F6' }} />
    </div>
  )
}
