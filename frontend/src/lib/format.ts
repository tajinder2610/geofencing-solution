import type { GeofenceCategory, LatLng } from '../api/types'

export const CATEGORIES: { value: GeofenceCategory; label: string; color: string }[] = [
  { value: 'delivery_zone', label: 'Delivery zone', color: '#2563eb' },
  { value: 'restricted_zone', label: 'Restricted zone', color: '#dc2626' },
  { value: 'toll_zone', label: 'Toll zone', color: '#d97706' },
  { value: 'customer_area', label: 'Customer area', color: '#059669' },
]

export const categoryLabel = (c: string) => CATEGORIES.find((x) => x.value === c)?.label ?? c
export const categoryColor = (c: string) => CATEGORIES.find((x) => x.value === c)?.color ?? '#6b7280'

export const VEHICLE_TYPES = ['truck', 'car', 'van', 'motorcycle', 'bus']

export function vehicleEmoji(type: string): string {
  switch (type.toLowerCase()) {
    case 'truck': return '🚚'
    case 'car': return '🚗'
    case 'van': return '🚐'
    case 'motorcycle': return '🏍️'
    case 'bus': return '🚌'
    default: return '🚘'
  }
}

export const formatDateTime = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })

export const formatCoord = (n: number) => n.toFixed(5)

/** Value for <input type="datetime-local"> in local time. */
export function toLocalInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** Convert a datetime-local value (local time) into a UTC ISO string. */
export const localInputToISO = (v: string) => new Date(v).toISOString()

/**
 * Parse "lat, lng" lines (also accepts "[lat, lng]," pasted from JSON). A trailing point equal to the
 * first (already-closed ring) is dropped because the UI closes the polygon itself.
 */
export function parseCoordinateText(text: string): { points: LatLng[]; error?: string } {
  const points: LatLng[] = []
  const lines = text.split('\n').map((l) => l.trim()).filter(Boolean)
  for (const [i, line] of lines.entries()) {
    const nums = line.match(/-?\d+(?:\.\d+)?/g)
    if (!nums || nums.length !== 2) return { points: [], error: `Line ${i + 1}: expected "latitude, longitude"` }
    const [lat, lng] = nums.map(Number)
    if (lat < -90 || lat > 90) return { points: [], error: `Line ${i + 1}: latitude must be between -90 and 90` }
    if (lng < -180 || lng > 180) return { points: [], error: `Line ${i + 1}: longitude must be between -180 and 180` }
    points.push([lat, lng])
  }
  if (points.length > 1) {
    const [f, l] = [points[0], points[points.length - 1]]
    if (f[0] === l[0] && f[1] === l[1]) points.pop()
  }
  return { points }
}

export const pointsToText = (pts: LatLng[]) => pts.map(([a, b]) => `${a}, ${b}`).join('\n')

/** Close the ring for the API. */
export const closeRing = (pts: LatLng[]): LatLng[] => (pts.length ? [...pts, pts[0]] : pts)
