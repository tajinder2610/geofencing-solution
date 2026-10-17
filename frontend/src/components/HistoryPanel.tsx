import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import type { Violation } from '../api/types'
import { useAppData } from '../hooks/useAppData'
import { categoryColor, categoryLabel, formatCoord, formatDateTime, localInputToISO } from '../lib/format'
import { Badge, Empty, ErrorBanner, Field, Spinner } from './ui'

interface Filters {
  vehicle_id: string
  geofence_id: string
  start: string
  end: string
}
const NO_FILTERS: Filters = { vehicle_id: '', geofence_id: '', start: '', end: '' }
const PAGE_SIZES = [10, 25, 50, 100]

/** `refreshKey` changes whenever a live alert arrives so the table stays current. */
export function HistoryPanel({ refreshKey }: { refreshKey: number }) {
  const { vehicles, geofences } = useAppData()

  const [draft, setDraft] = useState<Filters>(NO_FILTERS)
  const [applied, setApplied] = useState<Filters>(NO_FILTERS)
  const [pageSize, setPageSize] = useState(25)
  const [page, setPage] = useState(0)
  const [reloadKey, setReloadKey] = useState(0)

  const [rows, setRows] = useState<Violation[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    api
      .history({
        vehicle_id: applied.vehicle_id || undefined,
        geofence_id: applied.geofence_id || undefined,
        start_date: applied.start ? localInputToISO(applied.start) : undefined,
        end_date: applied.end ? localInputToISO(applied.end) : undefined,
        limit: pageSize,
        offset: page * pageSize,
      })
      .then((r) => {
        if (cancelled) return
        setRows(r.items)
        setTotal(r.total)
        setError(null)
      })
      .catch((e) => {
        if (!cancelled) setError(e)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [applied, page, pageSize, refreshKey, reloadKey])

  const apply = (e: FormEvent) => {
    e.preventDefault()
    setPage(0)
    setApplied(draft)
  }
  const reset = () => {
    setDraft(NO_FILTERS)
    setApplied(NO_FILTERS)
    setPage(0)
  }

  const pages = Math.max(1, Math.ceil(total / pageSize))
  const from = total === 0 ? 0 : page * pageSize + 1
  const to = Math.min(total, (page + 1) * pageSize)

  return (
    <section className="card">
      <h2>Violation history</h2>
      <form className="filters" onSubmit={apply}>
        <Field label="Vehicle">
          <select value={draft.vehicle_id} onChange={(e) => setDraft({ ...draft, vehicle_id: e.target.value })}>
            <option value="">All vehicles</option>
            {vehicles.map((v) => (
              <option key={v.id} value={v.id}>
                {v.vehicle_number}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Geofence">
          <select value={draft.geofence_id} onChange={(e) => setDraft({ ...draft, geofence_id: e.target.value })}>
            <option value="">All geofences</option>
            {geofences.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="From">
          <input type="datetime-local" value={draft.start} onChange={(e) => setDraft({ ...draft, start: e.target.value })} />
        </Field>
        <Field label="To">
          <input type="datetime-local" value={draft.end} onChange={(e) => setDraft({ ...draft, end: e.target.value })} />
        </Field>
        <div className="filter-actions">
          <button className="btn btn-primary">Apply</button>
          <button type="button" className="btn" onClick={reset}>
            Reset
          </button>
        </div>
      </form>

      <ErrorBanner error={error} />

      {loading && rows.length === 0 ? (
        <Spinner label="Loading history…" />
      ) : rows.length === 0 && !error ? (
        <Empty title="No events found">Events appear when a vehicle enters or leaves a geofence.</Empty>
      ) : (
        <div className="table-wrap">
          <table className={loading ? 'dim' : ''}>
            <thead>
              <tr>
                <th>Time</th>
                <th>Event</th>
                <th>Vehicle</th>
                <th>Geofence</th>
                <th>Location</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id}>
                  <td>{formatDateTime(r.timestamp)}</td>
                  <td>
                    <Badge color={r.event_type === 'entry' ? '#dc2626' : '#2563eb'}>{r.event_type === 'entry' ? '↘ Entry' : '↖ Exit'}</Badge>
                  </td>
                  <td>{r.vehicle_number}</td>
                  <td>
                    {r.geofence_name} <Badge color={categoryColor(r.category)}>{categoryLabel(r.category)}</Badge>
                  </td>
                  <td className="mono">
                    {formatCoord(r.latitude)}, {formatCoord(r.longitude)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="pager">
        <span className="muted">
          {from}–{to} of {total}
        </span>
        <span className="pager-controls">
          <select aria-label="Rows per page" value={pageSize} onChange={(e) => (setPageSize(Number(e.target.value)), setPage(0))}>
            {PAGE_SIZES.map((s) => (
              <option key={s} value={s}>
                {s} / page
              </option>
            ))}
          </select>
          <button className="btn btn-small" disabled={page === 0} onClick={() => setPage(page - 1)}>
            ← Prev
          </button>
          <span>
            Page {page + 1} / {pages}
          </span>
          <button className="btn btn-small" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>
            Next →
          </button>
          <button className="btn btn-small" onClick={() => setReloadKey((key) => key + 1)} disabled={loading}>
            ⟳
          </button>
        </span>
      </div>
    </section>
  )
}
