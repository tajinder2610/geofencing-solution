import { useEffect, useState, type FormEvent } from 'react'
import { api, type LocationUpdateResult } from '../api/client'
import type { LatLng, Vehicle } from '../api/types'
import { useAppData } from '../hooks/useAppData'
import { useToasts } from '../hooks/useToasts'
import { formatCoord, formatDateTime, localInputToISO, toLocalInputValue, VEHICLE_TYPES, vehicleEmoji } from '../lib/format'
import type { MapFocus } from './MapView'
import { Badge, Empty, ErrorBanner, Field, Spinner } from './ui'

interface Props {
  picked: LatLng | null
  onPickChange: (p: LatLng | null) => void
  selectedVehicleId: string | null
  onSelectVehicle: (id: string | null) => void
  onFocus: (f: MapFocus) => void
}

export function VehiclePanel({ picked, onPickChange, selectedVehicleId, onSelectVehicle, onFocus }: Props) {
  const { vehicles, loading, refreshVehicles } = useAppData()
  const { push } = useToasts()
  const [actionBusy, setActionBusy] = useState<string | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)

  // ---- register
  const [number, setNumber] = useState('')
  const [driver, setDriver] = useState('')
  const [type, setType] = useState('truck')
  const [phone, setPhone] = useState('')
  const [registering, setRegistering] = useState(false)
  const [registerError, setRegisterError] = useState<unknown>(null)

  const register = async (e: FormEvent) => {
    e.preventDefault()
    setRegisterError(null)
    setRegistering(true)
    try {
      const res = await api.createVehicle({ vehicle_number: number, driver_name: driver, vehicle_type: type, phone })
      push({ kind: 'success', title: 'Vehicle registered', body: res.vehicle_number })
      setNumber('')
      setDriver('')
      setPhone('')
      await refreshVehicles()
      onSelectVehicle(res.id)
    } catch (err) {
      setRegisterError(err)
    } finally {
      setRegistering(false)
    }
  }

  // ---- location update
  const [lat, setLat] = useState('')
  const [lng, setLng] = useState('')
  const [when, setWhen] = useState(() => toLocalInputValue(new Date()))
  const [updating, setUpdating] = useState(false)
  const [updateError, setUpdateError] = useState<unknown>(null)
  const [result, setResult] = useState<LocationUpdateResult | null>(null)

  // Clicking the map fills the inputs. Inputs that already represent the picked value are left
  // untouched, otherwise typing "-122." would be rewritten to "-122" mid-keystroke.
  useEffect(() => {
    if (!picked) return
    setLat((cur) => (Number(cur) === picked[0] && cur.trim() !== '' ? cur : String(picked[0])))
    setLng((cur) => (Number(cur) === picked[1] && cur.trim() !== '' ? cur : String(picked[1])))
  }, [picked])

  const onCoordInput = (nextLat: string, nextLng: string) => {
    setLat(nextLat)
    setLng(nextLng)
    const a = Number(nextLat)
    const b = Number(nextLng)
    if (nextLat.trim() && nextLng.trim() && Number.isFinite(a) && Number.isFinite(b) && Math.abs(a) <= 90 && Math.abs(b) <= 180) {
      onPickChange([a, b])
    }
  }

  const submitLocation = async (e: FormEvent) => {
    e.preventDefault()
    setUpdateError(null)
    setResult(null)
    if (!selectedVehicleId) return setUpdateError(new Error('Select a vehicle first.'))
    setUpdating(true)
    try {
      const res = await api.updateLocation({
        vehicle_id: selectedVehicleId,
        latitude: Number(lat),
        longitude: Number(lng),
        timestamp: localInputToISO(when),
      })
      setResult(res)
      await refreshVehicles()
      onFocus({ center: [Number(lat), Number(lng)], nonce: Date.now() })
    } catch (err) {
      setUpdateError(err)
    } finally {
      setUpdating(false)
    }
  }

  const selected = vehicles.find((v) => v.id === selectedVehicleId)
  const latNum = Number(lat)
  const lngNum = Number(lng)
  const coordsValid = lat.trim() !== '' && lng.trim() !== '' && Math.abs(latNum) <= 90 && Math.abs(lngNum) <= 180

  const focusVehicle = (v: Vehicle) => {
    onSelectVehicle(v.id)
    setResult(null)
    if (v.current_location) onFocus({ center: [v.current_location.latitude, v.current_location.longitude], nonce: Date.now() })
  }

  const clearTrail = async (v: Vehicle) => {
    if (!window.confirm(`Clear the trail and current location for ${v.vehicle_number}? Violation history will be kept.`)) return
    setActionBusy(v.id)
    setActionError(null)
    try {
      await api.clearVehicleTrail(v.id)
      push({ kind: 'success', title: 'Trail cleared', body: `${v.vehicle_number} location and trail removed; violation history kept.` })
      await refreshVehicles()
    } catch (err) {
      setActionError(err)
    } finally {
      setActionBusy(null)
    }
  }

  const removeVehicle = async (v: Vehicle) => {
    if (!window.confirm(`Delete ${v.vehicle_number}? Its trail, violation history, and vehicle-specific alert rules will also be deleted.`)) return
    setActionBusy(v.id)
    setActionError(null)
    try {
      await api.deleteVehicle(v.id)
      if (selectedVehicleId === v.id) {
        onSelectVehicle(null)
        onPickChange(null)
        setLat('')
        setLng('')
        setResult(null)
      }
      push({ kind: 'success', title: 'Vehicle deleted', body: v.vehicle_number })
      await refreshVehicles()
    } catch (err) {
      setActionError(err)
    } finally {
      setActionBusy(null)
    }
  }

  return (
    <div className="panel-stack">
      <section className="card">
        <h2>Update location</h2>
        <form onSubmit={submitLocation} className="form" noValidate>
          <ErrorBanner error={updateError} />
          <Field label="Vehicle">
            <select value={selectedVehicleId ?? ''} onChange={(e) => (onSelectVehicle(e.target.value || null), setResult(null))}>
              <option value="">Select a vehicle…</option>
              {vehicles.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.vehicle_number} — {v.driver_name}
                </option>
              ))}
            </select>
          </Field>
          <p className="hint">📍 Click anywhere on the map to set the position, or type coordinates.</p>
          <div className="row">
            <Field label="Latitude">
              <input inputMode="decimal" value={lat} onChange={(e) => onCoordInput(e.target.value, lng)} placeholder="37.7799" />
            </Field>
            <Field label="Longitude">
              <input inputMode="decimal" value={lng} onChange={(e) => onCoordInput(lat, e.target.value)} placeholder="-122.4144" />
            </Field>
          </div>
          <Field label="Timestamp">
            <div className="row">
              <input type="datetime-local" step={1} value={when} onChange={(e) => setWhen(e.target.value)} />
              <button type="button" className="btn" onClick={() => setWhen(toLocalInputValue(new Date()))}>
                Now
              </button>
            </div>
          </Field>
          <button className="btn btn-primary" disabled={updating || !selectedVehicleId || !coordsValid || !when}>
            {updating ? <Spinner label="Sending…" /> : 'Send location update'}
          </button>
        </form>

        {result && (
          <div className="result" role="status">
            <strong>
              {selected?.vehicle_number ?? result.vehicle_id} at {formatCoord(latNum)}, {formatCoord(lngNum)}
            </strong>
            {result.current_geofences.length === 0 ? (
              <p>Not inside any geofence.</p>
            ) : (
              <ul>
                {result.current_geofences.map((g) => (
                  <li key={g.geofence_id}>
                    Inside <strong>{g.geofence_name}</strong>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </section>

      <section className="card">
        <h2>Register vehicle</h2>
        <form onSubmit={register} className="form" noValidate>
          <ErrorBanner error={registerError} />
          <div className="row">
            <Field label="Vehicle number">
              <input value={number} onChange={(e) => setNumber(e.target.value)} placeholder="KA-01-AB-1234" />
            </Field>
            <Field label="Type">
              <select value={type} onChange={(e) => setType(e.target.value)}>
                {VEHICLE_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {vehicleEmoji(t)} {t}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <Field label="Driver name">
            <input value={driver} onChange={(e) => setDriver(e.target.value)} placeholder="John Doe" />
          </Field>
          <Field label="Phone">
            <input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+1234567890" />
          </Field>
          <button className="btn btn-primary" disabled={registering || !number.trim() || !driver.trim() || !phone.trim()}>
            {registering ? <Spinner label="Registering…" /> : 'Register vehicle'}
          </button>
        </form>
      </section>

      <section className="card">
        <h2>Fleet ({vehicles.length})</h2>
        <ErrorBanner error={actionError} />
        {loading && vehicles.length === 0 ? (
          <Spinner label="Loading vehicles…" />
        ) : vehicles.length === 0 ? (
          <Empty title="No vehicles registered">Register your first vehicle above.</Empty>
        ) : (
          <ul className="list">
            {vehicles.map((v) => {
              const inside = v.current_geofences ?? []
              return (
                <li key={v.id} className={`list-item ${v.id === selectedVehicleId ? 'active' : ''}`}>
                  <button className="list-hit" onClick={() => focusVehicle(v)}>
                    <span className="emoji">{vehicleEmoji(v.vehicle_type)}</span>
                    <span className="list-main">
                      <strong>{v.vehicle_number}</strong>
                      <small>
                        {v.driver_name} · {v.phone}
                      </small>
                      <small className="muted">
                        {v.current_location ? `Last seen ${formatDateTime(v.current_location.timestamp)}` : 'No location reported yet'}
                      </small>
                    </span>
                    {v.current_location ? (
                      inside.length > 0 ? (
                        <Badge color={inside.some((g) => g.category === 'restricted_zone') ? '#dc2626' : '#059669'}>
                          Inside: {inside.map((g) => g.geofence_name).join(', ')}
                        </Badge>
                      ) : (
                        <Badge color="#6b7280">Outside</Badge>
                      )
                    ) : (
                      <Badge color="#9ca3af">No fix</Badge>
                    )}
                  </button>
                  <span className="rule-actions">
                    <button className="btn btn-small" disabled={actionBusy !== null} onClick={() => void clearTrail(v)}>
                      Clear trail
                    </button>
                    <button className="btn btn-small btn-danger" disabled={actionBusy !== null} onClick={() => void removeVehicle(v)}>
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
