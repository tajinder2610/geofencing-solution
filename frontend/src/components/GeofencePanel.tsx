import L from 'leaflet'
import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import type { Geofence, GeofenceCategory, LatLng } from '../api/types'
import { useAppData } from '../hooks/useAppData'
import { useToasts } from '../hooks/useToasts'
import { CATEGORIES, categoryColor, categoryLabel, closeRing, parseCoordinateText, pointsToText } from '../lib/format'
import type { MapFocus } from './MapView'
import { Badge, Empty, ErrorBanner, Field, Spinner } from './ui'

interface Props {
  draft: LatLng[]
  onDraftChange: (pts: LatLng[]) => void
  drawing: boolean
  onDrawingChange: (v: boolean) => void
  selectedGeofenceId: string | null
  onSelectGeofence: (id: string | null) => void
  onFocus: (f: MapFocus) => void
}

const samePoints = (a: LatLng[], b: LatLng[]) => a.length === b.length && a.every((p, i) => p[0] === b[i][0] && p[1] === b[i][1])

export function GeofencePanel({ draft, onDraftChange, drawing, onDrawingChange, selectedGeofenceId, onSelectGeofence, onFocus }: Props) {
  const { geofenceVersion, refreshGeofences, refreshRules, refreshVehicles } = useAppData()
  const { push } = useToasts()

  // ---- create form
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState<GeofenceCategory>('restricted_zone')
  const [coordText, setCoordText] = useState('')
  const [coordError, setCoordError] = useState<string | undefined>()
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<unknown>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  // Keep the textarea in sync when the polygon is edited by clicking on the map.
  useEffect(() => {
    if (!samePoints(parseCoordinateText(coordText).points, draft)) {
      setCoordText(pointsToText(draft))
      setCoordError(undefined)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft])

  const resetForm = () => {
    setEditingId(null)
    setName('')
    setDescription('')
    setFormError(null)
    onDraftChange([])
    onDrawingChange(false)
  }

  const startEdit = (g: Geofence) => {
    setEditingId(g.id)
    setName(g.name)
    setDescription(g.description)
    setCategory(g.category)
    setFormError(null)
    // Drop the closing point: the UI re-closes the ring on submit.
    onDraftChange(g.coordinates.slice(0, -1))
    onSelectGeofence(g.id)
    onFocus({ bounds: L.latLngBounds(g.coordinates), nonce: Date.now() })
  }

  const remove = async (g: Geofence) => {
    if (!window.confirm(`Delete "${g.name}"? Its alert rules and event history are deleted too.`)) return
    try {
      await api.deleteGeofence(g.id)
      push({ kind: 'info', title: 'Geofence deleted', body: g.name })
      if (editingId === g.id) resetForm()
      if (selectedGeofenceId === g.id) onSelectGeofence(null)
      await Promise.all([refreshGeofences(), refreshRules(), refreshVehicles()])
    } catch (err) {
      push({ kind: 'error', title: 'Could not delete geofence', body: (err as Error).message })
    }
  }

  const onCoordText = (v: string) => {
    setCoordText(v)
    const r = parseCoordinateText(v)
    setCoordError(r.error)
    if (!r.error) onDraftChange(r.points)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setFormError(null)
    if (draft.length < 3) {
      setCoordError('A geofence needs at least 3 points — click on the map or type coordinates.')
      return
    }
    setSubmitting(true)
    try {
      const body = { name: name.trim(), description: description.trim(), category, coordinates: closeRing(draft) }
      if (editingId) {
        await api.updateGeofence(editingId, body)
        push({ kind: 'success', title: 'Geofence updated', body: body.name })
        await Promise.all([refreshGeofences(), refreshVehicles(), refreshRules()])
      } else {
        const res = await api.createGeofence(body)
        push({ kind: 'success', title: 'Geofence created', body: res.name })
        await refreshGeofences()
      }
      resetForm()
    } catch (err) {
      setFormError(err)
    } finally {
      setSubmitting(false)
    }
  }

  // ---- list with server-side category filter
  const [filter, setFilter] = useState<GeofenceCategory | ''>('')
  const [items, setItems] = useState<Geofence[]>([])
  const [loadingList, setLoadingList] = useState(true)
  const [listError, setListError] = useState<unknown>(null)

  useEffect(() => {
    let cancelled = false
    setLoadingList(true)
    api
      .listGeofences(filter)
      .then((r) => !cancelled && (setItems(r.items), setListError(null)))
      .catch((e) => !cancelled && setListError(e))
      .finally(() => !cancelled && setLoadingList(false))
    return () => {
      cancelled = true
    }
  }, [filter, geofenceVersion])

  const focusGeofence = (g: Geofence) => {
    onSelectGeofence(g.id)
    onFocus({ bounds: L.latLngBounds(g.coordinates), nonce: Date.now() })
  }

  return (
    <div className="panel-stack">
      <section className="card">
        <h2>{editingId ? 'Edit geofence' : 'New geofence'}</h2>
        <form onSubmit={submit} className="form" noValidate>
          <ErrorBanner error={formError} />
          <Field label="Name">
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Downtown Delivery Zone" required />
          </Field>
          <Field label="Description (optional)">
            <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What is this area for?" />
          </Field>
          <Field label="Category">
            <select value={category} onChange={(e) => setCategory(e.target.value as GeofenceCategory)}>
              {CATEGORIES.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </Field>

          <div className="draw-controls">
            <button type="button" className={drawing ? 'btn btn-primary' : 'btn'} onClick={() => onDrawingChange(!drawing)}>
              {drawing ? '✏️ Drawing — click the map' : '✏️ Draw on map'}
            </button>
            <button type="button" className="btn" disabled={draft.length === 0} onClick={() => onDraftChange(draft.slice(0, -1))}>
              Undo
            </button>
            <button type="button" className="btn" disabled={draft.length === 0} onClick={() => onDraftChange([])}>
              Clear
            </button>
          </div>

          <Field
            label={`Coordinates (${draft.length} point${draft.length === 1 ? '' : 's'})`}
            hint="One “latitude, longitude” per line. The polygon is closed automatically."
          >
            <textarea
              rows={5}
              value={coordText}
              onChange={(e) => onCoordText(e.target.value)}
              placeholder={'37.7749, -122.4194\n37.7849, -122.4194\n37.7849, -122.4094\n37.7749, -122.4094'}
              spellCheck={false}
            />
          </Field>
          {coordError && <div className="field-error">{coordError}</div>}

          <div className="row">
            <button className="btn btn-primary" disabled={submitting || !name.trim() || draft.length < 3}>
              {submitting ? <Spinner label={editingId ? 'Saving…' : 'Creating…'} /> : editingId ? 'Save changes' : 'Create geofence'}
            </button>
            {editingId && (
              <button type="button" className="btn" onClick={resetForm}>
                Cancel edit
              </button>
            )}
          </div>
        </form>
      </section>

      <section className="card">
        <div className="card-head">
          <h2>Geofences</h2>
          <select aria-label="Filter by category" value={filter} onChange={(e) => setFilter(e.target.value as GeofenceCategory | '')}>
            <option value="">All categories</option>
            {CATEGORIES.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        </div>
        <ErrorBanner error={listError} />
        {loadingList && items.length === 0 ? (
          <Spinner label="Loading geofences…" />
        ) : items.length === 0 ? (
          <Empty title="No geofences yet">Draw one on the map to get started.</Empty>
        ) : (
          <ul className="list">
            {items.map((g) => (
              <li key={g.id} className={`list-item ${g.id === selectedGeofenceId ? 'active' : ''}`}>
                <button className="list-hit" onClick={() => focusGeofence(g)}>
                  <span className="swatch" style={{ background: categoryColor(g.category) }} />
                  <span className="list-main">
                    <strong>{g.name}</strong>
                    {g.description && <small>{g.description}</small>}
                    <small className="muted">{g.coordinates.length - 1} vertices · {g.id}</small>
                  </span>
                  <Badge color={categoryColor(g.category)}>{categoryLabel(g.category)}</Badge>
                </button>
                <span className="rule-actions">
                  <button className="btn btn-small" onClick={() => startEdit(g)}>
                    Edit
                  </button>
                  <button className="btn btn-small btn-danger" onClick={() => void remove(g)}>
                    Delete
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
