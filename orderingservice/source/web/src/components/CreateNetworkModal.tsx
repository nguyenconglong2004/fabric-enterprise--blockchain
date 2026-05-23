import { useState } from 'react'
import { api } from '../api/rest'
import { useClusterStore } from '../store/cluster'

interface Props { onClose: () => void }

export function CreateNetworkModal({ onClose }: Props) {
  const [port, setPort] = useState(6000)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const setNodeList = useClusterStore(s => s.setNodeList)

  const create = async () => {
    setLoading(true)
    setError('')
    try {
      await api.createNetwork(port)
      const nodes = await api.listNodes()
      setNodeList(nodes)
      onClose()
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={modalStyle} onClick={e => e.stopPropagation()}>
        <h2 style={{ margin: '0 0 16px', color: '#F3F4F6' }}>Create Network</h2>
        <label style={labelStyle}>Leader Port</label>
        <input type="number" value={port} onChange={e => setPort(Number(e.target.value))}
          style={inputStyle} />
        {error && <p style={{ color: '#EF4444', fontSize: 12, margin: '8px 0 0' }}>{error}</p>}
        <div style={{ display: 'flex', gap: 8, marginTop: 20 }}>
          <button onClick={onClose} style={cancelBtn}>Cancel</button>
          <button onClick={create} disabled={loading} style={confirmBtn}>
            {loading ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  )
}

export function AddNodeModal({ onClose }: Props) {
  const nodesMap = useClusterStore(s => s.nodes)
  const nodes = Object.values(nodesMap)
  const defaultPort = nodes.length > 0 ? Math.max(...nodes.map(n => n.port)) + 1 : 6001
  const [port, setPort] = useState(defaultPort)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const setNodeList = useClusterStore(s => s.setNodeList)

  const add = async () => {
    setLoading(true)
    setError('')
    try {
      await api.addNode(port)
      const updated = await api.listNodes()
      setNodeList(updated)
      onClose()
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={modalStyle} onClick={e => e.stopPropagation()}>
        <h2 style={{ margin: '0 0 16px', color: '#F3F4F6' }}>Add Node</h2>
        <label style={labelStyle}>Port</label>
        <input type="number" value={port} onChange={e => setPort(Number(e.target.value))}
          style={inputStyle} />
        {error && <p style={{ color: '#EF4444', fontSize: 12, margin: '8px 0 0' }}>{error}</p>}
        <div style={{ display: 'flex', gap: 8, marginTop: 20 }}>
          <button onClick={onClose} style={cancelBtn}>Cancel</button>
          <button onClick={add} disabled={loading} style={confirmBtn}>
            {loading ? 'Adding…' : 'Add'}
          </button>
        </div>
      </div>
    </div>
  )
}

const overlayStyle: React.CSSProperties = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 100,
}
const modalStyle: React.CSSProperties = {
  background: '#1F2937', borderRadius: 8, padding: 24, minWidth: 280,
  border: '1px solid #374151',
}
const labelStyle: React.CSSProperties = { display: 'block', color: '#9CA3AF', fontSize: 12, marginBottom: 4 }
const inputStyle: React.CSSProperties = {
  width: '100%', background: '#111827', color: '#F3F4F6', border: '1px solid #374151',
  borderRadius: 4, padding: '6px 10px', fontSize: 14, boxSizing: 'border-box',
}
const cancelBtn: React.CSSProperties = {
  flex: 1, background: 'transparent', color: '#9CA3AF', border: '1px solid #374151',
  borderRadius: 4, padding: '8px', cursor: 'pointer',
}
const confirmBtn: React.CSSProperties = {
  flex: 1, background: '#3B82F6', color: 'white', border: 'none',
  borderRadius: 4, padding: '8px', cursor: 'pointer',
}
