import { useState, useEffect, useRef } from 'react'
import { useWebSocket } from './api/ws'
import { useClusterStore } from './store/cluster'
import { NetworkTopology } from './components/NetworkTopology'
import { Sidebar } from './components/Sidebar'
import { CreateNetworkModal, AddNodeModal } from './components/CreateNetworkModal'
import { api } from './api/rest'

const SIDEBAR_MIN = 220
const SIDEBAR_DEFAULT = 320
const MAIN_MIN = 300

export default function App() {
  useWebSocket()
  const connected = useClusterStore(s => s.connected)
  const setNodeList = useClusterStore(s => s.setNodeList)
  const nodesMap = useClusterStore(s => s.nodes)
  const nodes = Object.values(nodesMap)
  const [modal, setModal] = useState<'create' | 'add' | null>(null)

  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [resizing, setResizing] = useState(false)
  const resizeRef = useRef<{ startX: number; startWidth: number } | null>(null)

  // Load initial node list
  useEffect(() => {
    api.listNodes().then(setNodeList).catch(() => {})
  }, [setNodeList])

  useEffect(() => {
    if (!resizing) return
    const onMove = (e: MouseEvent) => {
      const r = resizeRef.current
      if (!r) return
      const dx = r.startX - e.clientX // dragging left increases width
      const maxWidth = Math.max(SIDEBAR_MIN, window.innerWidth - MAIN_MIN)
      const next = Math.max(SIDEBAR_MIN, Math.min(maxWidth, r.startWidth + dx))
      setSidebarWidth(next)
    }
    const onUp = () => {
      resizeRef.current = null
      setResizing(false)
    }
    const prevCursor = document.body.style.cursor
    const prevSelect = document.body.style.userSelect
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      document.body.style.cursor = prevCursor
      document.body.style.userSelect = prevSelect
    }
  }, [resizing])

  const startResize = (e: React.MouseEvent) => {
    if (e.button !== 0) return
    resizeRef.current = { startX: e.clientX, startWidth: sidebarWidth }
    setResizing(true)
  }

  return (
    <div style={{
      height: '100vh', display: 'flex', flexDirection: 'column',
      background: '#0F172A', color: '#F3F4F6', fontFamily: 'system-ui, sans-serif',
    }}>
      {/* Header */}
      <header style={{
        padding: '10px 20px', borderBottom: '1px solid #1F2937',
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 18, fontWeight: 700 }}>Raft Ordering Service</span>
          <span style={{
            fontSize: 11, padding: '2px 8px', borderRadius: 10,
            background: connected ? '#065F46' : '#7F1D1D',
            color: connected ? '#6EE7B7' : '#FCA5A5',
          }}>
            {connected ? 'connected' : 'disconnected'}
          </span>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={() => setModal('create')} disabled={nodes.length > 0} style={headerBtn('#10B981')}>
            + Create Network
          </button>
          <button onClick={() => setModal('add')} disabled={nodes.length === 0} style={headerBtn('#3B82F6')}>
            + Add Node
          </button>
        </div>
      </header>

      {/* Main content */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        <main style={{ flex: 1, padding: 16, overflow: 'hidden', position: 'relative', minWidth: 0 }}>
          <NetworkTopology />
        </main>
        {!sidebarCollapsed && (
          <div
            onMouseDown={startResize}
            style={{
              width: 5,
              cursor: 'col-resize',
              background: resizing ? '#3B82F6' : '#1F2937',
              flexShrink: 0,
              transition: resizing ? 'none' : 'background 120ms',
            }}
            title="Drag to resize sidebar"
          />
        )}
        <Sidebar
          width={sidebarWidth}
          collapsed={sidebarCollapsed}
          onToggleCollapse={() => setSidebarCollapsed(c => !c)}
        />
      </div>

      {modal === 'create' && <CreateNetworkModal onClose={() => setModal(null)} />}
      {modal === 'add' && <AddNodeModal onClose={() => setModal(null)} />}
    </div>
  )
}

function headerBtn(bg: string): React.CSSProperties {
  return {
    background: bg, color: 'white', border: 'none', borderRadius: 6,
    padding: '6px 14px', cursor: 'pointer', fontSize: 13, fontWeight: 500,
  }
}
