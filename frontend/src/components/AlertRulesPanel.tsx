import { useState, type FormEvent } from 'react'
import { api } from '../api/client'
import type { RuleEventType } from '../api/types'
import { useAppData } from '../hooks/useAppData'
import { useToasts } from '../hooks/useToasts'
import { categoryColor, formatDateTime } from '../lib/format'
import { Badge, Empty, ErrorBanner, Field, Spinner } from './ui'

const EVENT_LABEL: Record<RuleEventType, string> = { entry: 'Entry', exit: 'Exit', both: 'Entry & exit' }

export function AlertRulesPanel() {
  const { geofences, vehicles, rules, loading, refreshRules } = useAppData()
  const { push } = useToasts()

  const [geofenceId, setGeofenceId] = useState('')
  const [vehicleId, setVehicleId] = useState('')
  const [eventType, setEventType] = useState<RuleEventType>('entry')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      await api.configureAlert({ geofence_id: geofenceId, vehicle_id: vehicleId || undefined, event_type: eventType })
      push({ kind: 'success', title: 'Alert rule created' })
      await refreshRules()
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  const toggle = async (id: string, status: 'active' | 'inactive') => {
    setBusyId(id)
    try {
      await api.setAlertStatus(id, status === 'active' ? 'inactive' : 'active')
      await refreshRules()
    } catch (err) {
      push({ kind: 'error', title: 'Could not update rule', body: (err as Error).message })
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (id: string) => {
    if (!window.confirm('Delete this alert rule?')) return
    setBusyId(id)
    try {
      await api.deleteAlert(id)
      push({ kind: 'info', title: 'Alert rule deleted' })
      await refreshRules()
    } catch (err) {
      push({ kind: 'error', title: 'Could not delete rule', body: (err as Error).message })
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div className="two-col">
      <section className="card">
        <h2>Configure alert</h2>
        <form onSubmit={submit} className="form" noValidate>
          <ErrorBanner error={error} />
          <Field label="Geofence">
            <select value={geofenceId} onChange={(e) => setGeofenceId(e.target.value)}>
              <option value="">Select a geofence…</option>
              {geofences.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Vehicle" hint="Leave on “All vehicles” to alert for any vehicle.">
            <select value={vehicleId} onChange={(e) => setVehicleId(e.target.value)}>
              <option value="">All vehicles</option>
              {vehicles.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.vehicle_number} — {v.driver_name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Trigger on">
            <select value={eventType} onChange={(e) => setEventType(e.target.value as RuleEventType)}>
              <option value="entry">Entry</option>
              <option value="exit">Exit</option>
              <option value="both">Entry &amp; exit</option>
            </select>
          </Field>
          <button className="btn btn-primary" disabled={saving || !geofenceId}>
            {saving ? <Spinner label="Saving…" /> : 'Create alert rule'}
          </button>
        </form>
      </section>

      <section className="card">
        <h2>Alert rules ({rules.length})</h2>
        {loading && rules.length === 0 ? (
          <Spinner label="Loading rules…" />
        ) : rules.length === 0 ? (
          <Empty title="No alert rules">Create a rule to be notified live when vehicles enter or leave a geofence.</Empty>
        ) : (
          <ul className="list">
            {rules.map((r) => {
              const category = geofences.find((g) => g.id === r.geofence_id)?.category
              return (
                <li key={r.alert_id} className={`rule ${r.status === 'inactive' ? 'rule-off' : ''}`}>
                  <span className="swatch" style={{ background: category ? categoryColor(category) : '#9ca3af' }} />
                  <span className="list-main">
                    <strong>{r.geofence_name}</strong>
                    <small>
                      {r.vehicle_number ? `Vehicle ${r.vehicle_number}` : 'All vehicles'} · {EVENT_LABEL[r.event_type]}
                    </small>
                    <small className="muted">Created {formatDateTime(r.created_at)}</small>
                  </span>
                  <Badge color={r.status === 'active' ? '#059669' : '#6b7280'}>{r.status}</Badge>
                  <span className="rule-actions">
                    <button className="btn btn-small" disabled={busyId === r.alert_id} onClick={() => toggle(r.alert_id, r.status)}>
                      {r.status === 'active' ? 'Pause' : 'Resume'}
                    </button>
                    <button className="btn btn-small btn-danger" disabled={busyId === r.alert_id} onClick={() => remove(r.alert_id)}>
                      Delete
                    </button>
                  </span>
                </li>
              )
            })}
          </ul>
        )}
      </section>
    </div>
  )
}
