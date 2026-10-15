import type { ReactNode } from 'react'
import { ApiError } from '../api/client'

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="spinner-wrap" role="status">
      <span className="spinner" aria-hidden />
      {label && <span>{label}</span>}
    </span>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  )
}

export function ErrorBanner({ error }: { error: unknown }) {
  if (!error) return null
  const message = error instanceof Error ? error.message : String(error)
  const details = error instanceof ApiError ? error.details : []
  return (
    <div className="banner banner-error" role="alert">
      <strong>{message}</strong>
      {details.length > 0 && (
        <ul>
          {details.map((d) => (
            <li key={d}>{d}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div className="empty-title">{title}</div>
      {children && <div className="empty-body">{children}</div>}
    </div>
  )
}

export function Badge({ color, children }: { color: string; children: ReactNode }) {
  return (
    <span className="badge" style={{ background: color + '1a', color, borderColor: color + '55' }}>
      {children}
    </span>
  )
}
