import { useState } from 'react'
import { api } from '../api/rest'

interface Props { port: number }

export function NodeConnect({ port }: Props) {
  const [addr, setAddr] = useState('')
  const [result, setResult] = useState('')
  const [loading, setLoading] = useState(false)
  const [isError, setIsError] = useState(false)

  const connect = async () => {
    const a = addr.trim()
    if (!a) return
    setLoading(true)
    setResult('')
    try {
      const res = await api.execCmd(port, `connect ${a}`)
      setIsError(res.output.toLowerCase().startsWith('error'))
      setResult(res.output)
    } catch (e) {
      setIsError(true)
      setResult(String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') connect()
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <label style={{ color: '#9CA3AF', fontSize: 11 }}>Peer multiaddress</label>
      <input
        value={addr}
        onChange={e => setAddr(e.target.value)}
        onKeyDown={handleKey}
        placeholder="/ip4/127.0.0.1/tcp/9001/p2p/12D3Koo…"
        style={{
          background: '#1E293B', color: '#E2E8F0', border: '1px solid #374151',
          borderRadius: 4, padding: '6px 8px', fontFamily: 'monospace',
          fontSize: 11, outline: 'none',
        }}
      />
      <button
        onClick={connect}
        disabled={loading || !addr.trim()}
        style={{
          background: addr.trim() && !loading ? '#2563EB' : '#1E293B',
          color: addr.trim() && !loading ? '#fff' : '#6B7280',
          border: '1px solid #374151', borderRadius: 4,
          padding: '6px 14px', cursor: addr.trim() && !loading ? 'pointer' : 'default',
          fontSize: 12,
        }}
      >
        {loading ? 'Connecting…' : 'Connect'}
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
