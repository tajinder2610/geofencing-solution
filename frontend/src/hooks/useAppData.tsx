import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { AlertRule, Geofence, Vehicle } from '../api/types'

interface AppData {
  geofences: Geofence[]
  vehicles: Vehicle[]
  rules: AlertRule[]
  loading: boolean
  error: string | null
  /** Bumped whenever geofences change so list views can refetch their filtered query. */
  geofenceVersion: number
  refreshGeofences: () => Promise<void>
  refreshVehicles: () => Promise<void>
  refreshRules: () => Promise<void>
}

const AppDataContext = createContext<AppData | null>(null)
const VEHICLE_POLL_MS = 5000

export function AppDataProvider({ children }: { children: ReactNode }) {
  const [geofences, setGeofences] = useState<Geofence[]>([])
  const [vehicles, setVehicles] = useState<Vehicle[]>([])
  const [rules, setRules] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [geofenceVersion, setGeofenceVersion] = useState(0)

  const refreshGeofences = useCallback(async () => {
    const r = await api.listGeofences()
    setGeofences(r.items)
    setGeofenceVersion((v) => v + 1)
  }, [])
  const refreshVehicles = useCallback(async () => setVehicles((await api.listVehicles()).items), [])
  const refreshRules = useCallback(async () => setRules((await api.listAlerts()).items), [])

  useEffect(() => {
    let cancelled = false
    Promise.all([refreshGeofences(), refreshVehicles(), refreshRules()])
      .then(() => !cancelled && setError(null))
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [refreshGeofences, refreshVehicles, refreshRules])

  // Keep vehicle positions fresh: locations can be updated by other clients (e.g. a tracker or curl).
  const refreshVehiclesRef = useRef(refreshVehicles)
  refreshVehiclesRef.current = refreshVehicles
  useEffect(() => {
    const id = window.setInterval(() => {
      if (document.visibilityState === 'visible') refreshVehiclesRef.current().catch(() => undefined)
    }, VEHICLE_POLL_MS)
    return () => window.clearInterval(id)
  }, [])

  const value = useMemo(
    () => ({ geofences, vehicles, rules, loading, error, geofenceVersion, refreshGeofences, refreshVehicles, refreshRules }),
    [geofences, vehicles, rules, loading, error, geofenceVersion, refreshGeofences, refreshVehicles, refreshRules],
  )
  return <AppDataContext.Provider value={value}>{children}</AppDataContext.Provider>
}

export function useAppData(): AppData {
  const ctx = useContext(AppDataContext)
  if (!ctx) throw new Error('useAppData must be used inside <AppDataProvider>')
  return ctx
}
