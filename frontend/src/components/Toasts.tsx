import { useToasts } from '../hooks/useToasts'

const ICONS = { success: '✓', error: '!', info: 'i', entry: '↘', exit: '↖' } as const

export function Toasts() {
  const { toasts, dismiss } = useToasts()
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <button key={t.id} className={`toast toast-${t.kind}`} onClick={() => dismiss(t.id)} title="Dismiss">
          <span className="toast-icon">{ICONS[t.kind]}</span>
          <span className="toast-text">
            <strong>{t.title}</strong>
            {t.body && <span>{t.body}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
