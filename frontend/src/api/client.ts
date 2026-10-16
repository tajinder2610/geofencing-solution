import type {
  AlertRule,
  Geofence,
  GeofenceCategory,
  GeofenceRef,
  LatLng,
  Location,
  Page,
  RuleEventType,
  Vehicle,
  Violation,
} from './types'

export const API_URL = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080').replace(/\/$/, '')
const API_KEY: string | undefined = import.meta.env.VITE_API_KEY || undefined

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public details: string[] = [],
  ) {
    super(message)
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (init.body) headers['Content-Type'] = 'application/json'
  if (API_KEY) headers['X-API-Key'] = API_KEY

  let res: Response
  try {
    res = await fetch(API_URL + path, { ...init, headers })
  } catch {
    throw new ApiError(0, `Cannot reach the server at ${API_URL}. Is the backend running?`)
  }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new ApiError(res.status, data.error ?? `Request failed (${res.status})`, data.details ?? [])
  }
  return data as T
}

const qs = (params: Record<string, string | number | undefined | null>) => {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== '') sp.set(k, String(v))
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export interface LocationUpdateResult {
  vehicle_id: string
  location_updated: boolean
  current_geofences: GeofenceRef[]
}

export interface VehicleStatus {
  vehicle_id: string
  vehicle_number: string
  current_location: Location | null
  current_geofences: GeofenceRef[]
}

export const api = {
  async listGeofences(category?: GeofenceCategory | ''): Promise<Page<Geofence>> {
    const r = await request<{ geofences: Geofence[]; total_count: number }>(`/geofences${qs({ category, limit: 1000 })}`)
    return { items: r.geofences, total: r.total_count }
  },
  createGeofence(body: { name: string; description: string; category: GeofenceCategory; coordinates: LatLng[] }) {
    return request<{ id: string; name: string; status: string }>('/geofences', { method: 'POST', body: JSON.stringify(body) })
  },

  updateGeofence(id: string, body: { name: string; description: string; category: GeofenceCategory; coordinates: LatLng[] }) {
    return request<Geofence>(`/geofences/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) })
  },
  deleteGeofence(id: string) {
    return request<{ deleted: boolean }>(`/geofences/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },

  async listVehicles(): Promise<Page<Vehicle>> {
    const r = await request<{ vehicles: Vehicle[]; total_count: number }>(`/vehicles${qs({ include_location: 'true', limit: 1000 })}`)
    return { items: r.vehicles, total: r.total_count }
  },
  createVehicle(body: { vehicle_number: string; driver_name: string; vehicle_type: string; phone: string }) {
    return request<{ id: string; vehicle_number: string; status: string }>('/vehicles', { method: 'POST', body: JSON.stringify(body) })
  },
  clearVehicleTrail(id: string) {
    return request<{ cleared: boolean }>(`/vehicles/${encodeURIComponent(id)}/trail`, { method: 'DELETE' })
  },
  deleteVehicle(id: string) {
    return request<{ deleted: boolean }>(`/vehicles/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },
  updateLocation(body: { vehicle_id: string; latitude: number; longitude: number; timestamp: string }) {
    return request<LocationUpdateResult>('/vehicles/location', { method: 'POST', body: JSON.stringify(body) })
  },
  async vehicleTrail(id: string, limit = 100): Promise<Location[]> {
    const r = await request<{ points: Location[] }>(`/vehicles/trail/${encodeURIComponent(id)}${qs({ limit })}`)
    return r.points
  },
  vehicleStatus(id: string) {
    return request<VehicleStatus>(`/vehicles/location/${encodeURIComponent(id)}`)
  },

  async listAlerts(): Promise<Page<AlertRule>> {
    const r = await request<{ alerts: AlertRule[]; total_count: number }>(`/alerts${qs({ limit: 1000 })}`)
    return { items: r.alerts, total: r.total_count }
  },
  configureAlert(body: { geofence_id: string; vehicle_id?: string; event_type: RuleEventType }) {
    return request<{ alert_id: string }>('/alerts/configure', { method: 'POST', body: JSON.stringify(body) })
  },
  setAlertStatus(id: string, status: 'active' | 'inactive') {
    return request<AlertRule>(`/alerts/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ status }) })
  },
  deleteAlert(id: string) {
    return request<{ deleted: boolean }>(`/alerts/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },

  async history(f: {
    vehicle_id?: string
    geofence_id?: string
    start_date?: string
    end_date?: string
    limit: number
    offset: number
  }): Promise<Page<Violation>> {
    const r = await request<{ violations: Violation[]; total_count: number }>(`/violations/history${qs(f)}`)
    return { items: r.violations, total: r.total_count }
  },
}

/** WebSocket URL for live alerts. Browsers can't set headers on WS, so the key goes in the query string. */
export function alertsSocketUrl(): string {
  const override = import.meta.env.VITE_WS_URL as string | undefined
  const base = override || API_URL.replace(/^http/, 'ws') + '/ws/alerts'
  return API_KEY ? `${base}${base.includes('?') ? '&' : '?'}api_key=${encodeURIComponent(API_KEY)}` : base
}
