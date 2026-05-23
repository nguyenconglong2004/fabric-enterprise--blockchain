import { useEffect, useRef, useState } from 'react'
import { useClusterStore } from '../store/cluster'
import { NodeCircle } from './NodeCircle'
import { HeartbeatBeam } from './HeartbeatBeam'

const CX = 400
const CY = 300
const RING_R = 220
const NODE_R = 30
const VIEW_W = 800
const VIEW_H = 600
const ZOOM_MIN = 0.4
const ZOOM_MAX = 3

function nodePositions(count: number): { x: number; y: number }[] {
  if (count === 0) return []
  if (count === 1) return [{ x: CX, y: CY }]
  return Array.from({ length: count }, (_, i) => ({
    x: CX + RING_R * Math.cos((2 * Math.PI * i) / count - Math.PI / 2),
    y: CY + RING_R * Math.sin((2 * Math.PI * i) / count - Math.PI / 2),
  }))
}

// Re-render every 500ms to refresh countdown rings (local tick, not global store)
function useAnimationTick() {
  const [, setTick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 500)
    return () => clearInterval(id)
  }, [])
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}

export function NetworkTopology() {
  useAnimationTick()
  const nodesMap = useClusterStore(s => s.nodes)
  const nodes = Object.values(nodesMap)
  const beams = useClusterStore(s => s.beams)
  const selectedPort = useClusterStore(s => s.selectedPort)
  const setSelectedPort = useClusterStore(s => s.setSelectedPort)
  const globalTerm = useClusterStore(s => s.globalTerm)

  const positions = nodePositions(nodes.length)
  const portToPos = Object.fromEntries(nodes.map((n, i) => [n.port, positions[i]]))
  const leader = nodes.find(n => n.state === 'Leader')

  const svgRef = useRef<SVGSVGElement | null>(null)
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const dragRef = useRef<{ startX: number; startY: number; panX: number; panY: number } | null>(null)
  const [dragging, setDragging] = useState(false)

  // Convert client mouse pos → SVG viewBox coords (accounting for preserveAspectRatio="xMidYMid meet")
  const clientToView = (clientX: number, clientY: number) => {
    const svg = svgRef.current
    if (!svg) return { x: VIEW_W / 2, y: VIEW_H / 2 }
    const rect = svg.getBoundingClientRect()
    const scale = Math.min(rect.width / VIEW_W, rect.height / VIEW_H)
    const offsetX = (rect.width - VIEW_W * scale) / 2
    const offsetY = (rect.height - VIEW_H * scale) / 2
    return {
      x: (clientX - rect.left - offsetX) / scale,
      y: (clientY - rect.top - offsetY) / scale,
    }
  }

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    const factor = e.deltaY < 0 ? 1.1 : 1 / 1.1
    const newZoom = clamp(zoom * factor, ZOOM_MIN, ZOOM_MAX)
    if (newZoom === zoom) return
    const m = clientToView(e.clientX, e.clientY)
    // Keep point under cursor fixed: world_p = (m - pan)/zoom must stay constant.
    // newPan = m - (m - pan) * (newZoom / zoom)
    const ratio = newZoom / zoom
    setPan({
      x: m.x - (m.x - pan.x) * ratio,
      y: m.y - (m.y - pan.y) * ratio,
    })
    setZoom(newZoom)
  }

  const handleMouseDown = (e: React.MouseEvent) => {
    // Only start pan on background (the SVG itself), not on child nodes/beams
    if (e.target !== svgRef.current) return
    if (e.button !== 0) return
    dragRef.current = { startX: e.clientX, startY: e.clientY, panX: pan.x, panY: pan.y }
    setDragging(true)
  }

  useEffect(() => {
    if (!dragging) return
    const onMove = (e: MouseEvent) => {
      const d = dragRef.current
      if (!d) return
      const svg = svgRef.current
      if (!svg) return
      const rect = svg.getBoundingClientRect()
      const scale = Math.min(rect.width / VIEW_W, rect.height / VIEW_H)
      const dx = (e.clientX - d.startX) / scale
      const dy = (e.clientY - d.startY) / scale
      setPan({ x: d.panX + dx, y: d.panY + dy })
    }
    const onUp = () => {
      dragRef.current = null
      setDragging(false)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
  }, [dragging])

  const zoomBy = (factor: number) => {
    const newZoom = clamp(zoom * factor, ZOOM_MIN, ZOOM_MAX)
    if (newZoom === zoom) return
    // Zoom toward viewBox center
    const m = { x: VIEW_W / 2, y: VIEW_H / 2 }
    const ratio = newZoom / zoom
    setPan({
      x: m.x - (m.x - pan.x) * ratio,
      y: m.y - (m.y - pan.y) * ratio,
    })
    setZoom(newZoom)
  }

  const resetView = () => {
    setZoom(1)
    setPan({ x: 0, y: 0 })
  }

  return (
    <div style={{ position: 'relative', width: '100%', height: '100%' }}>
      <svg
        ref={svgRef}
        width="100%"
        height="100%"
        viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
        preserveAspectRatio="xMidYMid meet"
        style={{
          background: '#111827',
          borderRadius: 8,
          cursor: dragging ? 'grabbing' : 'grab',
          display: 'block',
        }}
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
      >
        <g transform={`translate(${pan.x} ${pan.y}) scale(${zoom})`}>
          {/* Edges from leader to followers */}
          {leader && positions.length > 0 &&
            nodes.filter(n => n.port !== leader.port && n.state !== 'Offline').map(n => {
              const from = portToPos[leader.port]
              const to = portToPos[n.port]
              if (!from || !to) return null
              return (
                <line
                  key={n.port}
                  x1={from.x} y1={from.y}
                  x2={to.x} y2={to.y}
                  stroke="#374151" strokeWidth={1.5}
                />
              )
            })
          }

          {/* Heartbeat beams */}
          {beams.map(beam => {
            const from = portToPos[beam.fromPort]
            const to = portToPos[beam.toPort]
            if (!from || !to) return null
            return (
              <HeartbeatBeam
                key={beam.id}
                beam={beam}
                fromX={from.x} fromY={from.y}
                toX={to.x} toY={to.y}
              />
            )
          })}

          {/* Global term */}
          <text x={CX} y={CY - 12} textAnchor="middle" fontSize={14} fill="#6B7280" fontFamily="monospace">TERM</text>
          <text x={CX} y={CY + 18} textAnchor="middle" fontSize={36} fontWeight="bold" fill="#F3F4F6" fontFamily="monospace">
            {globalTerm}
          </text>

          {/* Node circles */}
          {nodes.map((node, i) => (
            <NodeCircle
              key={node.port}
              node={node}
              cx={positions[i].x}
              cy={positions[i].y}
              r={NODE_R}
              selected={selectedPort === node.port}
              hbTimeoutMs={node.hbTimeoutMs}
              onClick={() => setSelectedPort(selectedPort === node.port ? null : node.port)}
            />
          ))}

          {/* Empty state */}
          {nodes.length === 0 && (
            <text x={CX} y={CY + 40} textAnchor="middle" fontSize={14} fill="#6B7280">
              No nodes — click &quot;Create Network&quot; to start
            </text>
          )}
        </g>
      </svg>

      {/* Zoom controls overlay */}
      <div style={{
        position: 'absolute', right: 12, bottom: 12,
        display: 'flex', alignItems: 'center', gap: 4,
        background: 'rgba(31, 41, 55, 0.92)',
        border: '1px solid #374151',
        borderRadius: 6,
        padding: 4,
        userSelect: 'none',
      }}>
        <button onClick={() => zoomBy(1 / 1.2)} style={zoomBtn} title="Zoom out">−</button>
        <button onClick={resetView} style={{ ...zoomBtn, width: 'auto', padding: '0 8px', fontSize: 11 }} title="Reset view">
          {Math.round(zoom * 100)}%
        </button>
        <button onClick={() => zoomBy(1.2)} style={zoomBtn} title="Zoom in">+</button>
      </div>
    </div>
  )
}

const zoomBtn: React.CSSProperties = {
  width: 28, height: 24,
  background: 'transparent',
  color: '#F3F4F6',
  border: '1px solid #4B5563',
  borderRadius: 4,
  cursor: 'pointer',
  fontSize: 14,
  fontWeight: 600,
  display: 'flex', alignItems: 'center', justifyContent: 'center',
}
