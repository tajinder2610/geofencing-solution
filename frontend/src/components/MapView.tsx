import L, { type LatLngBoundsExpression } from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { useEffect, useMemo, useRef } from 'react'
import { CircleMarker, MapContainer, Marker, Polygon, Polyline, Popup, TileLayer, Tooltip, useMap, useMapEvents } from 'react-leaflet'
import type { Geofence, LatLng, Vehicle } from '../api/types'
import { categoryColor, categoryLabel, formatDateTime, vehicleEmoji } from '../lib/format'

export type MapMode = 'view' | 'draw' | 'pick'

export interface MapFocus {
  bounds?: LatLngBoundsExpression
  center?: LatLng
  nonce: number
}

interface Props {
  geofences: Geofence[]
  vehicles: Vehicle[]
  mode: MapMode
  draft: LatLng[]
  onDraftAdd: (p: LatLng) => void
  picked: LatLng | null
  onPick: (p: LatLng) => void
  focus: MapFocus | null
  trail?: LatLng[]
  selectedVehicleId?: string | null
  selectedGeofenceId?: string | null
  onSelectVehicle?: (id: string) => void
  /** A vehicle that just triggered an alert: its marker pulses. */
  pulse?: { vehicleId: string; nonce: number } | null
}

const DEFAULT_CENTER: LatLng = [37.7799, -122.4144]

function ClickHandler({ mode, onDraftAdd, onPick }: Pick<Props, 'mode' | 'onDraftAdd' | 'onPick'>) {
  useMapEvents({
    click(e) {
      const p: LatLng = [Number(e.latlng.lat.toFixed(6)), Number(e.latlng.lng.toFixed(6))]
      if (mode === 'draw') onDraftAdd(p)
      else if (mode === 'pick') onPick(p)
    },
  })
  return null
}

/** Pans/zooms when `focus` changes, and fits all geofences once when they first load. */
function Viewport({ focus, geofences }: { focus: MapFocus | null; geofences: Geofence[] }) {
  const map = useMap()
  const didInitialFit = useRef(false)

  useEffect(() => {
    if (didInitialFit.current || geofences.length === 0) return
    didInitialFit.current = true
    const pts = geofences.flatMap((g) => g.coordinates)
    map.fitBounds(L.latLngBounds(pts), { padding: [40, 40], maxZoom: 15 })
  }, [geofences, map])

  useEffect(() => {
    if (!focus) return
    if (focus.bounds) map.fitBounds(focus.bounds, { padding: [50, 50], maxZoom: 16 })
    else if (focus.center) map.flyTo(focus.center, Math.max(map.getZoom(), 15))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focus?.nonce])

  return null
}

function vehicleIcon(v: Vehicle, selected: boolean, pulsing: boolean) {
  const inRestricted = v.current_geofences?.some((g) => g.category === 'restricted_zone')
  const cls = ['veh-marker', selected ? 'selected' : '', inRestricted ? 'restricted' : '', pulsing ? 'pulse' : '']
    .filter(Boolean)
    .join(' ')
  return L.divIcon({
    className: 'veh-marker-wrap',
    html: `<div class="${cls}"><span>${vehicleEmoji(v.vehicle_type)}</span></div>`,
    iconSize: [38, 38],
    iconAnchor: [19, 19],
  })
}

export function MapView(props: Props) {
  const { geofences, vehicles, mode, draft, picked, focus, trail, selectedVehicleId, selectedGeofenceId, onSelectVehicle, pulse } = props

  const located = useMemo(() => vehicles.filter((v) => v.current_location), [vehicles])

  return (
    <MapContainer center={DEFAULT_CENTER} zoom={13} className={`map map-${mode}`} scrollWheelZoom>
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
      />
      <Viewport focus={focus} geofences={geofences} />
      <ClickHandler mode={mode} onDraftAdd={props.onDraftAdd} onPick={props.onPick} />

      {geofences.map((g) => {
        const color = categoryColor(g.category)
        const selected = g.id === selectedGeofenceId
        return (
          <Polygon
            // While drawing/picking, polygons ignore the mouse so clicks inside a zone reach the map
            // (no popup covering the spot). `interactive` is only read at creation, hence it is part of the key.
            key={`${g.id}-${mode === 'view'}`}
            positions={g.coordinates}
            interactive={mode === 'view'}
            pathOptions={{ color, weight: selected ? 4 : 2, fillColor: color, fillOpacity: selected ? 0.3 : 0.15 }}
          >
            <Tooltip sticky>{g.name}</Tooltip>
            <Popup>
              <strong>{g.name}</strong>
              <br />
              {categoryLabel(g.category)}
              {g.description && (
                <>
                  <br />
                  {g.description}
                </>
              )}
            </Popup>
          </Polygon>
        )
      })}

      {located.map((v) => {
        const loc = v.current_location!
        const selected = v.id === selectedVehicleId
        const pulsing = pulse?.vehicleId === v.id
        return (
          <Marker
            // The nonce in the key re-creates the marker so its CSS pulse animation restarts.
            key={`${v.id}-${pulsing ? pulse!.nonce : 0}`}
            position={[loc.latitude, loc.longitude]}
            icon={vehicleIcon(v, selected, pulsing)}
            eventHandlers={{ click: () => onSelectVehicle?.(v.id) }}
            zIndexOffset={selected ? 1000 : 0}
          >
            <Popup>
              <strong>{v.vehicle_number}</strong>
              <br />
              {v.driver_name} · {v.vehicle_type}
              <br />
              <small>{formatDateTime(loc.timestamp)}</small>
              <br />
              {v.current_geofences && v.current_geofences.length > 0
                ? `Inside: ${v.current_geofences.map((g) => g.geofence_name).join(', ')}`
                : 'Not inside any geofence'}
            </Popup>
          </Marker>
        )
      })}

      {trail && trail.length > 1 && (
        <>
          <Polyline positions={trail} pathOptions={{ color: '#7c3aed', weight: 3, opacity: 0.8 }} />
          <CircleMarker center={trail[0]} radius={5} pathOptions={{ color: '#7c3aed', fillColor: '#fff', fillOpacity: 1 }}>
            <Tooltip direction="top">Trail start</Tooltip>
          </CircleMarker>
        </>
      )}

      {mode === 'draw' && draft.length > 0 && (
        <>
          {draft.length >= 3 ? (
            <Polygon positions={draft} pathOptions={{ color: '#7c3aed', weight: 2, dashArray: '6 6', fillOpacity: 0.12 }} />
          ) : (
            <Polyline positions={draft} pathOptions={{ color: '#7c3aed', weight: 2, dashArray: '6 6' }} />
          )}
          {draft.map((p, i) => (
            <CircleMarker key={i} center={p} radius={i === 0 ? 7 : 5} pathOptions={{ color: '#7c3aed', fillColor: i === 0 ? '#fff' : '#7c3aed', fillOpacity: 1 }} />
          ))}
        </>
      )}

      {picked && (
        <CircleMarker center={picked} radius={9} pathOptions={{ color: '#111827', weight: 3, fillColor: '#facc15', fillOpacity: 1 }}>
          <Tooltip permanent direction="top" offset={[0, -8]}>
            New location
          </Tooltip>
        </CircleMarker>
      )}
    </MapContainer>
  )
}
