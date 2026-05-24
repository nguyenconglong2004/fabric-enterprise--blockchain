import { useState } from 'react'
import { useClusterStore } from '../store/cluster'
import { NodeTerminal } from './NodeTerminal'
import { NodeStatus } from './NodeStatus'
import { NodeConnect } from './NodeConnect'
import { NodeDelay } from './NodeDelay'
import { ConfigPanel } from './ConfigPanel'
import { api } from '../api/rest'

interface SidebarProps {
  width?: number
  collapsed?: boolean
  onToggleCollapse?: () => void
}

const COLLAPSED_W = 40

export function Sidebar({ width = 320, collapsed = false, onToggleCollapse }: SidebarProps) {
  const selectedPort = useClusterStore(s => s.selectedPort)
  const node = useClusterStore(s => selectedPort ? s.nodes[selectedPort] : null)
  const [tab, setTab] = useState<'logs' | 'status' | 'connect' | 'delay' | 'config'>('logs')

  if (collapsed) {
    return (
      <aside style={{ ...baseStyle, width: COLLAPSED_W, minWidth: COLLAPSED_W }}>
        <button
          onClick={onToggleCollapse}
          title="Expand sidebar"
          style={collapseBtn}
        >▶</button>
      </aside>
    )
  }

  if (!node) {
    return (
      <aside style={{ ...baseStyle, width, minWidth: width }}>
        <div style={{ display: 'flex', justifyContent: 'flex-end', padding: '8px 8px 0' }}>
          <button onClick={onToggleCollapse} title="Collapse sidebar" style={collapseBtn}>◀</button>
        </div>
        <p style={{ color: '#6B7280', padding: 16, fontSize: 13 }}>
          Click a node to inspect it.
        </p>
      </aside>
    )
  }

  const remove = async () => {
    if (!confirm(`Stop node :${node.port}?`)) return
    await api.removeNode(node.port)
    useClusterStore.getState().setSelectedPort(null)
  }

  return (
    <aside style={{ ...baseStyle, width, minWidth: width }}>
      <div style={{ padding: '12px 16px', borderBottom: '1px solid #1F2937' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
          <h3 style={{ margin: 0, color: '#F3F4F6', fontSize: 15 }}>Node :{node.port}</h3>
          <div style={{ display: 'flex', gap: 6 }}>
            <button onClick={remove} style={{
              background: 'transparent', border: '1px solid #EF4444', color: '#EF4444',
              borderRadius: 4, padding: '2px 8px', cursor: 'pointer', fontSize: 11,
            }}>Stop</button>
            <button onClick={onToggleCollapse} title="Collapse sidebar" style={collapseBtn}>◀</button>
          </div>
        </div>
        <table style={{ marginTop: 8, fontSize: 12, color: '#D1D5DB', borderSpacing: '0 2px', width: '100%' }}>
          <tbody>
            <tr><td style={{ color: '#6B7280' }}>State</td><td style={{ paddingLeft: 8 }}>{node.state}</td></tr>
            <tr><td style={{ color: '#6B7280' }}>Term</td><td style={{ paddingLeft: 8 }}>{node.term}</td></tr>
            <tr><td style={{ color: '#6B7280' }}>PeerID</td><td style={{ paddingLeft: 8, wordBreak: 'break-all', fontSize: 10 }}>{node.peerID.slice(0, 20)}…</td></tr>
          </tbody>
        </table>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', borderBottom: '1px solid #1F2937' }}>
        {(['logs', 'status', 'connect', 'delay', 'config'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)} style={{
            flex: 1, padding: '7px 0', background: tab === t ? '#1F2937' : 'transparent',
            color: tab === t ? '#F3F4F6' : '#6B7280', border: 'none', cursor: 'pointer',
            fontSize: 11, textTransform: 'capitalize',
          }}>{t}</button>
        ))}
      </div>

      <div style={{ padding: 12, overflow: 'auto', flex: 1 }}>
        {tab === 'logs'    && <NodeTerminal key={node.port} port={node.port} />}
        {tab === 'status'  && <NodeStatus   key={node.port} port={node.port} />}
        {tab === 'connect' && <NodeConnect  key={node.port} port={node.port} />}
        {tab === 'delay'   && <NodeDelay    key={node.port} port={node.port} />}
        {tab === 'config'  && <ConfigPanel  key={node.port} port={node.port} />}
      </div>
    </aside>
  )
}

const baseStyle: React.CSSProperties = {
  background: '#111827',
  borderLeft: '1px solid #1F2937',
  display: 'flex',
  flexDirection: 'column',
  overflow: 'hidden',
  flexShrink: 0,
}

const collapseBtn: React.CSSProperties = {
  background: 'transparent',
  border: '1px solid #374151',
  color: '#9CA3AF',
  borderRadius: 4,
  padding: '2px 6px',
  cursor: 'pointer',
  fontSize: 11,
  lineHeight: 1,
}
