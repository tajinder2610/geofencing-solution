import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'

export type ToastKind = 'success' | 'error' | 'info' | 'entry' | 'exit'

export interface Toast {
  id: number
  kind: ToastKind
  title: string
  body?: string
}

interface ToastApi {
  toasts: Toast[]
  push: (t: Omit<Toast, 'id'>, ttlMs?: number) => void
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastApi | null>(null)
let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const dismiss = useCallback((id: number) => setToasts((ts) => ts.filter((t) => t.id !== id)), [])
  const push = useCallback(
    (t: Omit<Toast, 'id'>, ttlMs = 6000) => {
      const id = nextId++
      setToasts((ts) => [...ts.slice(-4), { ...t, id }]) // keep at most 5 on screen
      window.setTimeout(() => dismiss(id), ttlMs)
    },
    [dismiss],
  )

  const value = useMemo(() => ({ toasts, push, dismiss }), [toasts, push, dismiss])
  return <ToastContext.Provider value={value}>{children}</ToastContext.Provider>
}

export function useToasts(): ToastApi {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToasts must be used inside <ToastProvider>')
  return ctx
}
