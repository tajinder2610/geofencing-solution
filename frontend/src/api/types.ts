export type GeofenceCategory = 'delivery_zone' | 'restricted_zone' | 'toll_zone' | 'customer_area'
export type EventType = 'entry' | 'exit'
export type RuleEventType = EventType | 'both'
export type LatLng = [number, number]

export interface Geofence {
  id: string
  name: string
  description: string
  coordinates: LatLng[]
  category: GeofenceCategory
  created_at: string
}

export interface GeofenceRef {
  geofence_id: string
  geofence_name: string
  category?: GeofenceCategory
  status?: string
}

export interface Location {
  latitude: number
  longitude: number
  timestamp: string
}

export interface Vehicle {
  id: string
  vehicle_number: string
  driver_name: string
  vehicle_type: string
  phone: string
  status: string
  created_at: string
  current_location?: Location
  current_geofences?: GeofenceRef[]
}

export interface AlertRule {
  alert_id: string
  geofence_id: string
  geofence_name: string
  vehicle_id: string | null
  vehicle_number: string | null
  event_type: RuleEventType
  status: 'active' | 'inactive'
  created_at: string
}

export interface Violation {
  id: string
  vehicle_id: string
  vehicle_number: string
  geofence_id: string
  geofence_name: string
  category: GeofenceCategory
  event_type: EventType
  latitude: number
  longitude: number
  timestamp: string
}

/** Message pushed over /ws/alerts. */
export interface LiveAlert {
  event_id: string
  event_type: EventType
  timestamp: string
  vehicle: { vehicle_id: string; vehicle_number: string; driver_name: string }
  geofence: { geofence_id: string; geofence_name: string; category: GeofenceCategory }
  location: { latitude: number; longitude: number }
}

export interface Page<T> {
  items: T[]
  total: number
}
