import { useState } from 'react'
import { api } from '../api/rest'
import { useClusterStore } from '../store/cluster'

interface Props { port: number }

export function NodeDelay({ port }: Props) {
  const node = useClusterStore(s => s.nodes[port])
  const isLeader = node?.state === 'Leader'

  const [seconds, setSeconds] = useState('5')
  const [priorities, setPriorities] = useState('0')
  const [result, setResult] = useState('')
  const [loading, setLoading] = useState(false)
  const [isError, setIsError] = useState(false)

  const apply = async () => {
    const secs = seconds.trim()
    const prios = priorities.trim().replace(/,/g, ' ').replace(/\s+/g, ' ')
    if (!secs || !prios) return
    setLoading(true)
    setResult('')
    try {
      const res = await api.execCmd(port, `delay ${secs} ${prios}`)
      setIsError(res.output.toLowerCase().startsWith('error') || res.output.toLowerCase().startsWith('invalid'))
      setResult(res.output)
    } catch (e) {
      setIsError(true)
      setResult(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {!isLeader && (
        <div style={{
          background: '#451A03', border: '1px solid #92400E', borderRadius: 4,
          padding: '6px 10px', color: '#FCD34D', fontSize: 11,
        }}>
          Only the leader can apply delay. Current state: {node?.state ?? '—'}
        </div>
      )}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <label style={{ color: '#9CA3AF', fontSize: 11 }}>Delay (seconds)</label>
        <input
          type="number"
          min={1}
          value={seconds}
          onChange={e => setSeconds(e.target.value)}
          disabled={!isLeader}
          style={{
            background: '#1E293B', color: isLeader ? '#E2E8F0' : '#6B7280',
            border: '1px solid #374151', borderRadius: 4,
            padding: '6px 8px', fontSize: 12, outline: 'none', width: 80,
          }}
        />
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <label style={{ color: '#9CA3AF', fontSize: 11 }}>
          Target priorities{' '}
          <span style={{ color: '#6B7280' }}>(space or comma separated)</span>
        </label>
        <input
          value={priorities}
          onChange={e => setPriorities(e.target.value)}
          placeholder="0 1 2"
          disabled={!isLeader}
          style={{
            background: '#1E293B', color: isLeader ? '#E2E8F0' : '#6B7280',
            border: '1px solid #374151', borderRadius: 4,
            padding: '6px 8px', fontFamily: 'monospace', fontSize: 12,
            outline: 'none',
          }}
        />
      </div>
      <button
        onClick={apply}
        disabled={!isLeader || loading}
        style={{
          background: isLeader && !loading ? '#D97706' : '#1E293B',
          color: isLeader && !loading ? '#000' : '#6B7280',
          border: '1px solid #374151', borderRadius: 4,
          padding: '6px 14px', cursor: isLeader && !loading ? 'pointer' : 'default',
          fontSize: 12, fontWeight: 600,
        }}
      >
        {loading ? 'Applying…' : 'Apply Delay'}
      </button>
      {result && (
        <pre style={{
          background: '#0F172A', color: isError ? '#F87171' : '#4ADE80',
          borderRadius: 4, padding: '6px 10px', fontSize: 11,
          fontFamily: 'monospace', margin: 0, whiteSpace: 'pre-wrap',
          border: `1px solid ${isError ? '#7F1D1D' : '#14532D'}`,
        }}>
          {result}
        </pre>
      )}
    </div>
  )
}
