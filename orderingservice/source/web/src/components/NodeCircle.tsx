import React from 'react'
import { type NodeData } from '../store/cluster'

const STATE_COLOR: Record<string, string> = {
  Leader: '#F59E0B',
  ClaimingLeader: '#EF6C00',
  Syncing: '#8B5CF6',
  Follower: '#3B82F6',
  Offline: '#6B7280',
}

const STATE_ICON: Record<string, string> = {
  Leader: '👑',
  ClaimingLeader: '⚡',
  Syncing: '🔄',
  Follower: '●',
  Offline: '💤',
}

interface Props {
  node: NodeData
  cx: number
  cy: number
  r: number
  selected: boolean
  hbTimeoutMs: number
  onClick: () => void
}

export function NodeCircle({ node, cx, cy, r, selected, hbTimeoutMs, onClick }: Props) {
  const color = STATE_COLOR[node.state] ?? '#6B7280'
  const icon = STATE_ICON[node.state] ?? '●'
  const opacity = node.state === 'Offline' ? 0.4 : 1

  // Countdown ring: fraction of timeout elapsed since last HB
  const elapsed = Date.now() - node.lastHbAt
  const fraction = Math.min(elapsed / hbTimeoutMs, 1)
  const ringColor = fraction < 0.5 ? '#3B82F6' : fraction < 0.8 ? '#F59E0B' : '#EF4444'
  const circumference = 2 * Math.PI * (r + 6)
  const dashOffset = circumference * fraction

  return (
    <g
      transform={`translate(${cx},${cy})`}
      onClick={onClick}
      style={{ cursor: 'pointer', opacity }}
    >
      {/* Countdown ring (followers only) */}
      {(node.state === 'Follower' || node.state === 'Syncing') && (
        <circle
          r={r + 6}
          fill="none"
          stroke={ringColor}
          strokeWidth={3}
          strokeDasharray={`${circumference - dashOffset} ${dashOffset}`}
          transform="rotate(-90)"
          opacity={0.7}
        />
      )}

      {/* Leader glow */}
      {node.state === 'Leader' && (
        <circle r={r + 10} fill={color} opacity={0.15} />
      )}

      {/* Selection ring */}
      {selected && (
        <circle r={r + 14} fill="none" stroke="white" strokeWidth={2} opacity={0.6} />
      )}

      {/* Main circle */}
      <circle r={r} fill={color} stroke="white" strokeWidth={selected ? 3 : 1.5} />

      {/* Icon */}
      <text textAnchor="middle" dominantBaseline="central" fontSize={r * 0.7} fill="white" pointerEvents="none">
        {icon}
      </text>

      {/* Port label */}
      <text y={r + 16} textAnchor="middle" fontSize={11} fill="#E5E7EB" fontFamily="monospace">
        :{node.port}
      </text>

      {/* Term label */}
      <text y={r + 28} textAnchor="middle" fontSize={10} fill="#9CA3AF" fontFamily="monospace">
        t{node.term}
      </text>
    </g>
  )
}
