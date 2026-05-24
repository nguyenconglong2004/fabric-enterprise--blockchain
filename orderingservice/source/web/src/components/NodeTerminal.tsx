import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useClusterStore } from '../store/cluster'

const EMPTY: string[] = []

interface Props {
  port: number
}

export function NodeTerminal({ port }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const logs = useClusterStore(s => s.logs[port] ?? EMPTY)
  const writtenLogsRef = useRef(0)

  useEffect(() => {
    if (!containerRef.current) return
    const term = new Terminal({
      theme: { background: '#0F172A', foreground: '#E2E8F0', cursor: '#F59E0B' },
      fontSize: 12,
      fontFamily: 'monospace',
      rows: 20,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    return () => term.dispose()
  }, [])

  useEffect(() => {
    const term = termRef.current
    if (!term) return
    for (let i = writtenLogsRef.current; i < logs.length; i++) {
      term.writeln('\x1b[36m' + logs[i] + '\x1b[0m')
    }
    writtenLogsRef.current = logs.length
  }, [logs])

  return (
    <div ref={containerRef} style={{ borderRadius: 4, overflow: 'hidden' }} />
  )
}
