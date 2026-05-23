import { useEffect, useRef } from 'react'
import { useClusterStore } from '../store/cluster'

const WS_URL = `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws/events`

export function useWebSocket() {
  const wsRef = useRef<WebSocket | null>(null)
  const handleEvent = useClusterStore(s => s.handleEvent)
  const setConnected = useClusterStore(s => s.setConnected)

  useEffect(() => {
    let retryDelay = 1000

    function connect() {
      const ws = new WebSocket(WS_URL)
      wsRef.current = ws

      ws.onopen = () => {
        setConnected(true)
        retryDelay = 1000
      }
      ws.onclose = () => {
        setConnected(false)
        setTimeout(connect, retryDelay)
        retryDelay = Math.min(retryDelay * 2, 10000)
      }
      ws.onerror = () => ws.close()
      ws.onmessage = e => {
        try {
          handleEvent(JSON.parse(e.data))
        } catch {
          // ignore malformed
        }
      }
    }

    connect()
    return () => { wsRef.current?.close() }
  }, [handleEvent, setConnected])
}
