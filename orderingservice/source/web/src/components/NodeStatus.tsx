import { useEffect, useState } from 'react'
import { api } from '../api/rest'

interface Props { port: number }

export function NodeStatus({ port }: Props) {
  const [output, setOutput] = useState('')
  const [loading, setLoading] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      const res = await api.execCmd(port, 'status')
      setOutput(res.output)
    } catch (e) {
      setOutput(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { refresh() }, [port])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <button
        onClick={refresh}
        disabled={loading}
        style={{
          background: '#1E293B', color: loading ? '#6B7280' : '#F59E0B',
          border: '1px solid #374151', borderRadius: 4,
          padding: '5px 12px', cursor: loading ? 'default' : 'pointer',
          fontSize: 12, alignSelf: 'flex-start',
        }}
      >
        {loading ? 'Loading…' : 'Refresh'}
      </button>
      {output && (
        <pre style={{
          background: '#0F172A', color: '#E2E8F0', borderRadius: 4,
          padding: '8px 10px', fontSize: 11, fontFamily: 'monospace',
          margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word',
          border: '1px solid #1F2937',
        }}>
          {output}
        </pre>
      )}
    </div>
  )
}
