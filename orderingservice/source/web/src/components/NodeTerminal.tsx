import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useClusterStore } from '../store/cluster'
import { api } from '../api/rest'

const EMPTY: string[] = []

interface Props {
  port: number
}

export function NodeTerminal({ port }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const [input, setInput] = useState('')
  const [history, setHistory] = useState<string[]>([])
  const [histIdx, setHistIdx] = useState(-1)
  const logs = useClusterStore(s => s.logs[port] ?? EMPTY)
  const cmdOutputs = useClusterStore(s => s.cmdOutputs[port] ?? EMPTY)
  const writtenLogsRef = useRef(0)
  const writtenCmdsRef = useRef(0)

  useEffect(() => {
    if (!containerRef.current) return
    const term = new Terminal({
      theme: { background: '#0F172A', foreground: '#E2E8F0', cursor: '#F59E0B' },
      fontSize: 12,
      fontFamily: 'monospace',
      rows: 16,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    return () => term.dispose()
  }, [])

  // Stream new log lines into terminal
  useEffect(() => {
    const term = termRef.current
    if (!term) return
    for (let i = writtenLogsRef.current; i < logs.length; i++) {
      term.writeln('\x1b[36m' + logs[i] + '\x1b[0m')
    }
    writtenLogsRef.current = logs.length
  }, [logs])

  // Print cmd outputs
  useEffect(() => {
    const term = termRef.current
    if (!term) return
    for (let i = writtenCmdsRef.current; i < cmdOutputs.length; i++) {
      term.writeln('\x1b[32m' + cmdOutputs[i] + '\x1b[0m')
    }
    writtenCmdsRef.current = cmdOutputs.length
  }, [cmdOutputs])

  const submit = async () => {
    const cmd = input.trim()
    if (!cmd) return
    setHistory(h => [cmd, ...h.slice(0, 49)])
    setHistIdx(-1)
    setInput('')
    termRef.current?.writeln('\x1b[33m> ' + cmd + '\x1b[0m')
    try {
      const { output } = await api.execCmd(port, cmd)
      if (output) termRef.current?.writeln('\x1b[32m' + output + '\x1b[0m')
    } catch (e) {
      termRef.current?.writeln('\x1b[31m' + String(e) + '\x1b[0m')
    }
  }

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') { submit(); return }
    if (e.key === 'ArrowUp') {
      const idx = Math.min(histIdx + 1, history.length - 1)
      setHistIdx(idx)
      setInput(history[idx] ?? '')
    }
    if (e.key === 'ArrowDown') {
      const idx = Math.max(histIdx - 1, -1)
      setHistIdx(idx)
      setInput(idx < 0 ? '' : history[idx])
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <div ref={containerRef} style={{ borderRadius: 4, overflow: 'hidden' }} />
      <div style={{ display: 'flex', gap: 4 }}>
        <span style={{ color: '#F59E0B', fontFamily: 'monospace', alignSelf: 'center' }}>&gt;</span>
        <input
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKey}
          placeholder="status | connect <addr> | delay <s> <p>"
          style={{
            flex: 1, background: '#1E293B', color: '#E2E8F0', border: '1px solid #374151',
            borderRadius: 4, padding: '4px 8px', fontFamily: 'monospace', fontSize: 12,
            outline: 'none',
          }}
        />
        <button onClick={submit} style={{
          background: '#F59E0B', color: '#000', border: 'none', borderRadius: 4,
          padding: '4px 10px', cursor: 'pointer', fontSize: 12,
        }}>Run</button>
      </div>
    </div>
  )
}
