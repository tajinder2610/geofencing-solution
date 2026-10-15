import { useCallback, useEffect, useState } from 'react'
import { api } from './api/client'
import type { LatLng, LiveAlert } from './api/types'
import { AlertFeed } from './components/AlertFeed'
import { AlertRulesPanel } from './components/AlertRulesPanel'
import { GeofencePanel } from './components/GeofencePanel'
import { HistoryPanel } from './components/HistoryPanel'
import { MapView, type MapFocus, type MapMode } from './components/MapView'
import { Toasts } from './components/Toasts'
import { VehiclePanel } from './components/VehiclePanel'
import { useAlertSocket, type SocketStatus } from './hooks/useAlertSocket'
import { useAppData } from './hooks/useAppData'
import { useToasts } from './hooks/useToasts'
import { categoryLabel } from './lib/format'

type Tab = 'geofences' | 'vehicles' | 'rules' | 'alerts' | 'history'

const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: 'geofences', label: 'Geofences', icon: '🗺️' },
  { id: 'vehicles', label: 'Vehicles', icon: '🚚' },
  { id: 'rules', label: 'Alert rules', icon: '⚙️' },
  { id: 'alerts', label: 'Live alerts', icon: '🔔' },
  { id: 'history', label: 'History', icon: '🕒' },
]

const MAX_FEED = 200
const STATUS_LABEL: Record<SocketStatus, string> = { open: 'Live', connecting: 'Connecting…', reconnecting: 'Reconnecting…' }

export default function App() {
  const { geofences, vehicles, error, refreshVehicles } = useAppData()
  const { push } = useToasts()

  const [tab, setTab] = useState<Tab>('geofences')
  const [alerts, setAlerts] = useState<LiveAlert[]>([])
  const [unread, setUnread] = useState(0)
  const [alertSeq, setAlertSeq] = useState(0) // monotonic; drives History auto-refresh (the feed itself is capped)
  const [pulse, setPulse] = useState<{ vehicleId: string; nonce: number } | null>(null)

  // map state shared by the Geofences and Vehicles tabs
  const [draft, setDraft] = useState<LatLng[]>([])
  const [drawing, setDrawing] = useState(false)
  const [picked, setPicked] = useState<LatLng | null>(null)
  const [focus, setFocus] = useState<MapFocus | null>(null)
  const [selectedVehicleId, setSelectedVehicleId] = useState<string | null>(null)
  const [selectedGeofenceId, setSelectedGeofenceId] = useState<string | null>(null)
  const [trail, setTrail] = useState<LatLng[]>([])

  const handleAlert = useCallback(
    (a: LiveAlert) => {
      setAlerts((prev) => (prev.some((x) => x.event_id === a.event_id) ? prev : [a, ...prev].slice(0, MAX_FEED)))
      setUnread((n) => n + 1)
      setAlertSeq((n) => n + 1)
      setPulse({ vehicleId: a.vehicle.vehicle_id, nonce: Date.now() })
      push(
        {
          kind: a.event_type,
          title: `${a.vehicle.vehicle_number} ${a.event_type === 'entry' ? 'entered' : 'exited'} ${a.geofence.geofence_name}`,
          body: `${categoryLabel(a.geofence.category)} · driver ${a.vehicle.driver_name}`,
        },
        9000,
      )
      void refreshVehicles().catch(() => undefined)
    },
    [push, refreshVehicles],
  )
  const socketStatus = useAlertSocket(handleAlert)

  // Draw the selected vehicle's recent path; refetch when it reports a newer position.
  const selectedStamp = vehicles.find((v) => v.id === selectedVehicleId)?.current_location?.timestamp
  useEffect(() => {
    if (!selectedVehicleId) return setTrail([])
    let cancelled = false
    api
      .vehicleTrail(selectedVehicleId, 100)
      .then((pts) => !cancelled && setTrail(pts.map((p) => [p.latitude, p.longitude] as LatLng)))
      .catch(() => !cancelled && setTrail([]))
    return () => {
      cancelled = true
    }
  }, [selectedVehicleId, selectedStamp])

  const openTab = (t: Tab) => {
    setTab(t)
    if (t === 'alerts') setUnread(0)
    if (t !== 'geofences') setDrawing(false)
  }

  const showMap = tab === 'geofences' || tab === 'vehicles'
  const mode: MapMode = tab === 'geofences' ? (drawing ? 'draw' : 'view') : tab === 'vehicles' ? 'pick' : 'view'

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-icon">📍</span>
          <div>
            <h1>Geofence Control Center</h1>
            <small>Real-time vehicle tracking &amp; alerts</small>
          </div>
        </div>
        <nav className="tabs" role="tablist">
          {TABS.map((t) => (
            <button key={t.id} role="tab" aria-selected={tab === t.id} className={`tab ${tab === t.id ? 'active' : ''}`} onClick={() => openTab(t.id)}>
              <span aria-hidden>{t.icon}</span>
              <span className="tab-label">{t.label}</span>
              {t.id === 'alerts' && unread > 0 && tab !== 'alerts' && <span className="tab-count">{unread > 99 ? '99+' : unread}</span>}
            </button>
          ))}
        </nav>
        <div className={`conn conn-${socketStatus}`} title="WebSocket connection to /ws/alerts">
          <span className="dot" />
          {STATUS_LABEL[socketStatus]}
        </div>
      </header>

      {error && (
        <div className="banner banner-error app-banner" role="alert">
          <strong>Could not load data.</strong> {error}
        </div>
      )}

      <main className={`workspace ${showMap ? 'with-map' : 'no-map'}`}>
        <div className="panel">
          {tab === 'geofences' && (
            <GeofencePanel
              draft={draft}
              onDraftChange={setDraft}
              drawing={drawing}
              onDrawingChange={setDrawing}
              selectedGeofenceId={selectedGeofenceId}
              onSelectGeofence={setSelectedGeofenceId}
              onFocus={setFocus}
            />
          )}
          {tab === 'vehicles' && (
            <VehiclePanel
              picked={picked}
              onPickChange={setPicked}
              selectedVehicleId={selectedVehicleId}
              onSelectVehicle={setSelectedVehicleId}
              onFocus={setFocus}
            />
          )}
          {tab === 'rules' && <AlertRulesPanel />}
          {tab === 'alerts' && <AlertFeed alerts={alerts} status={socketStatus} onClear={() => setAlerts([])} />}
          {tab === 'history' && <HistoryPanel refreshKey={alertSeq} />}
        </div>

        {showMap && (
          <div className="map-pane">
            <MapView
              geofences={geofences}
              vehicles={vehicles}
              mode={mode}
              draft={draft}
              onDraftAdd={(p) => setDraft((d) => [...d, p])}
              picked={tab === 'vehicles' ? picked : null}
              onPick={setPicked}
              focus={focus}
              trail={tab === 'vehicles' ? trail : undefined}
              selectedVehicleId={selectedVehicleId}
              selectedGeofenceId={selectedGeofenceId}
              onSelectVehicle={setSelectedVehicleId}
              pulse={pulse}
            />
            <Legend />
          </div>
        )}
      </main>
      <Toasts />
    </div>
  )
}

function Legend() {
  return (
    <div className="legend">
      <span><i style={{ background: '#2563eb' }} /> Delivery</span>
      <span><i style={{ background: '#dc2626' }} /> Restricted</span>
      <span><i style={{ background: '#d97706' }} /> Toll</span>
      <span><i style={{ background: '#059669' }} /> Customer</span>
    </div>
  )
}
