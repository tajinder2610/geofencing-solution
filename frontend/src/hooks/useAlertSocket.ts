import { useEffect, useRef, useState } from 'react'
import { alertsSocketUrl } from '../api/client'
import type { LiveAlert } from '../api/types'

export type SocketStatus = 'connecting' | 'open' | 'reconnecting'

const MIN_DELAY_MS = 1000
const MAX_DELAY_MS = 15000

/**
 * Maintains a WebSocket to /ws/alerts, reconnecting with exponential backoff.
 * `onAlert` may change between renders without causing a reconnect.
 */
export function useAlertSocket(onAlert: (alert: LiveAlert) => void): SocketStatus {
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const handlerRef = useRef(onAlert)
  handlerRef.current = onAlert

  useEffect(() => {
    let socket: WebSocket | null = null
    let timer: number | undefined
    let delay = MIN_DELAY_MS
    let disposed = false

    const connect = () => {
      setStatus((s) => (s === 'open' ? 'reconnecting' : s))
      socket = new WebSocket(alertsSocketUrl())

      socket.onopen = () => {
        delay = MIN_DELAY_MS
        setStatus('open')
      }
      socket.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data as string) as LiveAlert
          if (msg && msg.event_id && msg.geofence && msg.vehicle) handlerRef.current(msg)
        } catch {
          /* ignore malformed frames */
        }
      }
      socket.onclose = () => {
        if (disposed) return
        setStatus('reconnecting')
        timer = window.setTimeout(connect, delay)
        delay = Math.min(delay * 2, MAX_DELAY_MS)
      }
      socket.onerror = () => socket?.close()
    }

    connect()
    return () => {
      disposed = true
      window.clearTimeout(timer)
      socket?.close()
    }
  }, [])

  return status
}
